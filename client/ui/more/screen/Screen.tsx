import { cleanup, computed, effect, platform, Scroll, signal, Text, untracked, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { Button, Menu, MenuItem } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { GuardedSheet as Sheet } from "../../shell/AppSheet";
import { wide } from "../../shell/layout";
import { NavPage, PageStack } from "../../shell/NavPage";
import { failureText } from "../../../core/more/daemon";
import { directHint, SLOW_NOTE_MS, SlowWatch } from "../../../core/screen/connectionWatch";
import { screenRequest, type ScreenAction, type ScreenAgent, type ScreenStatus } from "../../../core/screen/screen";
import {
  connectionProblem,
  diagnosticRows,
  indicatorTone,
  powerSummary,
  powerWarning,
  screenAgents,
  screenEntry,
  unsupportedScreen,
  type IndicatorTone,
  type StreamReadout,
} from "../../../core/screen/screenPageModel";
import type { ScreenPointerMode } from "../../../core/screen/screenViewerProtocol";
import { changeIPhone, getIPhoneStatus, type IPhoneChange, type IPhoneDaemonStatus } from "../../../core/settings/iphoneStatus";
import { phoneAvailable, phoneStatus, readPhone } from "../../phone/state";
import { daemon } from "../daemon";
import { EmptyState, PillButton, Row, Section, Spinner } from "../../kit/kit";
import { tile } from "../../kit/RowTile";
import { Icon, type IconName } from "../../kit/Icon";
import { inFront } from "../../kit/inFront";
import type { PageNav } from "../routes";
import { AgentDisplays } from "./Displays";
import { canShowFullSize, modifiersFor, needsSoftKeyboard, RemoteSession, RemoteView } from "./RemoteView";
import { VirtualScreenSwitch, virtualScreenOn } from "./VirtualScreen";

/** Window height (pt) below which a page header does not fit beside the picture: a phone held sideways. */
const ROOMY_MIN_HEIGHT = 500;

type Phase = "checking" | "failed" | "setup" | "agent" | "local" | "live";
type How = "auto" | "takeOver" | "watch";

const MENU_POPUP = "rounded-[18px] bg-popover p-1.5 shadow-float";
/** On wide Screen ⋯ is a popover at its button, 288 wide (SPEC §14.2). */
const menuPopup = () => (wide() ? `${MENU_POPUP} w-[288px]` : MENU_POPUP);
const MENU_BACKDROP = "bg-scrim";
const ITEM = "min-h-[48px] justify-center rounded-xl px-4 py-2.5 hover:bg-accent active:bg-accent focus:bg-accent disabled:opacity-40";

/**
 * Screen: the current computer's own screen (Mac, Windows), or on Linux each
 * agent's display. Exactly one computer: a switch drops everything and reads
 * the next. Leaving the page, the app going to the background or a switch
 * closes the viewer; returning to the app looks again, and waits for an agent
 * at work instead of taking over.
 */
export function ScreenPage(props: { nav: PageNav; worker?: string }) {
  // The menu's Phone item says who uses the phone now.
  void readPhone();
  const status = signal<ScreenStatus | null>(null);
  const phase = signal<Phase>("checking");
  /** Why the last step failed, or the computer's own reason while setting up. */
  const reason = signal("");
  const busy = signal(false);
  const watching = signal<{ worker?: string } | null>(null);
  const session = signal<RemoteSession | null>(null);
  const typing = signal(false);
  const mode = signal<ScreenPointerMode>("touch");
  const details = signal(false);
  const slow = signal(false);
  const slowNote = signal(false);
  const iphone = signal<IPhoneDaemonStatus | null>(null);
  const iphoneBusy = signal(false);
  const slowWatch = new SlowWatch();
  let noteTimer: ReturnType<typeof setTimeout> | null = null;
  let epoch = 0;
  let serverId = "";
  let sessions = 0;
  let gone = false;

  const server = () => daemon()?.server ?? null;
  const isCurrent = (id: string) => untracked(server)?.id === id;
  const title = () => status()?.name || server()?.name || "this computer";

  const clearNote = () => {
    if (noteTimer) clearTimeout(noteTimer);
    noteTimer = null;
    slowNote.set(false);
  };
  const dropSession = () => {
    const current = untracked(session);
    if (current) {
      current.disconnect();
      session.set(null);
    }
    typing.set(false);
    slow.set(false);
    clearNote();
  };

  // A person comes first, so viewing ends an agent's control. Only an explicit
  // "takeOver" does that; opening the page waits for the agent, and "watch"
  // shows the agent's screen without taking over.
  const connect = async (how: How = "auto", worker?: string) => {
    const target = untracked(server);
    // A step that finishes after the page was left (Restart Mewla, Stop) opens nothing.
    if (!target || gone) return;
    dropSession();
    const token = ++epoch;
    const active = () => token === epoch && isCurrent(target.id);
    slowWatch.reset();
    watching.set(null);
    phase.set("checking");
    reason.set("");
    let next: ScreenStatus;
    try {
      next = await screenRequest(target);
    } catch (failure) {
      if (!active()) return;
      status.set(null);
      reason.set(failureText(failure, "Screen request failed"));
      phase.set("failed");
      return;
    }
    if (!active()) return;
    status.set(next);
    const entry = screenEntry(next, how);
    if (entry === "setup") {
      reason.set(next.reason || "");
      phase.set("setup");
      return;
    }
    // On the computer itself the screen is the one in front of the person: no live view, no stream.
    if (entry !== "live") {
      phase.set(entry);
      return;
    }
    const watch = how === "watch" && !!next.agent;
    watching.set(watch ? { worker } : null);
    const viewer = new RemoteSession(target, {
      watch,
      worker: watch ? worker : undefined,
      align: untracked(immersive) ? "center" : "top",
      // The watched agent finished: look again, which opens the screen.
      onSocketClosed: () => {
        if (!watch) return false;
        void connect();
        return true;
      },
    });
    viewer.mode = untracked(mode);
    viewer.locked.set(!!next.locked);
    sessions++;
    session.set(viewer);
    phase.set("live");
    viewer.connect();
  };

  const request = async (action: ScreenAction): Promise<ScreenStatus | null> => {
    const target = untracked(server);
    if (!target) return null;
    busy.set(true);
    try {
      const next = await screenRequest(target, action);
      if (isCurrent(target.id)) status.set(next);
      return next;
    } catch (failure) {
      if (isCurrent(target.id)) reason.set(failureText(failure, "Screen request failed"));
      return null;
    } finally {
      busy.set(false);
    }
  };
  const setKeepAwake = (keep: boolean) => void request({ keep_awake: keep });
  // Turning Screen on waits for someone at the computer to click Allow; the
  // operating system then asks for its own permissions there.
  const enable = async () => {
    if (untracked(busy)) return;
    reason.set("");
    const next = await request({ action: "enable" });
    if (next?.ready) void connect();
  };
  const openSettings = async (permission: string) => {
    const target = untracked(server);
    if (!target) return;
    try {
      await screenRequest(target, { action: "open_settings", permission });
    } catch (failure) {
      reason.set(failureText(failure, "Screen request failed"));
    }
  };
  // Restarting drops this connection; keep checking until Mewla answers.
  const restartMewla = async () => {
    const target = untracked(server);
    if (!target || untracked(busy)) return;
    busy.set(true);
    reason.set("");
    try {
      await screenRequest(target, { action: "restart" });
      for (let attempt = 0; attempt < 30 && isCurrent(target.id) && !gone; attempt++) {
        await new Promise((resolve) => setTimeout(resolve, 2_000));
        try {
          const next = await screenRequest(target);
          if (!isCurrent(target.id)) return;
          status.set(next);
          if (!next.permissions?.some((p) => p.restart_required)) break;
        } catch {
          // Still restarting.
        }
      }
    } catch (failure) {
      if (isCurrent(target.id)) reason.set(failureText(failure, "Screen request failed"));
    } finally {
      busy.set(false);
    }
    if (isCurrent(target.id)) void connect();
  };
  const stopAgent = async (worker?: string) => {
    if (untracked(busy)) return;
    await request(worker ? { action: "stop_agent", worker } : { action: "stop_agent" });
    void connect();
  };
  const stopWatching = () => {
    dropSession();
    watching.set(null);
    if (screenAgents(untracked(status)).length) phase.set("agent");
    else void connect();
  };
  const changeIPhoneUse = async (change: IPhoneChange) => {
    const target = untracked(server);
    if (!target || untracked(iphoneBusy)) return;
    iphoneBusy.set(true);
    try {
      const next = await changeIPhone(target, change);
      if (isCurrent(target.id)) iphone.set(next);
    } catch {
      // The next read says what happened.
    } finally {
      iphoneBusy.set(false);
    }
  };

  // The computer: a switch drops what the last one said and reads the next.
  effect(() => {
    const target = server();
    untracked(() => {
      const id = target?.id ?? "";
      if (id === serverId) return;
      serverId = id;
      epoch++;
      dropSession();
      status.set(null);
      iphone.set(null);
      watching.set(null);
      reason.set("");
      phase.set("checking");
      if (target) void connect();
    });
  });

  // Away from the app: no picture streams. Back: look again.
  let foreground = untracked(inFront);
  effect(() => {
    const active = inFront();
    untracked(() => {
      if (active === foreground) return;
      foreground = active;
      if (!server()) return;
      if (!active) {
        epoch++;
        session()?.disconnect();
        clearNote();
      } else void connect();
    });
  });

  // Leaving the page closes the viewer: an open one counts as a person at the computer.
  cleanup(() => {
    gone = true;
    epoch++;
    dropSession();
    if (noteTimer) clearTimeout(noteTimer);
  });

  const linuxDisplays = () => {
    const s = status();
    const target = server();
    return !!s && !!target && s.platform === "linux" && (!!s.agent_displays || virtualScreenOn(target.id));
  };

  // While an allowed device waits for permissions, follow the computer: the
  // person there turns switches on, and Mewla may restart to apply them. While
  // an agent uses the screen, look again until it is done, then open; on the
  // computer itself the card just follows the agents. Live, the power state.
  effect(() => {
    const p = phase();
    const s = status();
    const target = server();
    if (!target || !inFront() || linuxDisplays()) return;
    const waitingForPermissions = p === "setup" && !!s?.supported && !s.disabled && s.approved && !s.ready;
    if (!waitingForPermissions && p !== "agent" && p !== "local" && p !== "live") return;
    const timer = setInterval(() => {
      void screenRequest(target)
        .then((next) => {
          if (!isCurrent(target.id) || untracked(phase) !== p) return;
          status.set(next);
          if (waitingForPermissions && next.ready) void connect();
          if (p === "agent" && screenAgents(next).length === 0) void connect();
        })
        .catch(() => {
          // Restarting, or a moment away: the next look says.
        });
    }, p === "live" ? 30_000 : 3_000);
    return () => clearInterval(timer);
  });

  // An agent on the iPhone works in iPhone Mirroring, off the Mac's screen: Watch
  // can't show it, so it has its own Done (for what it asked of the person) and Stop.
  // A browser has no iPhone use (as in the Expo web app): it never asks.
  effect(() => {
    const p = phase();
    const target = server();
    if (platform.os === "web" || !target || (p !== "agent" && p !== "local") || !inFront()) return;
    const read = () =>
      void getIPhoneStatus(target)
        .then((next) => isCurrent(target.id) && iphone.set(next))
        .catch(() => isCurrent(target.id) && iphone.set(null));
    read();
    const timer = setInterval(read, 5_000);
    return () => clearInterval(timer);
  });

  // Held sideways the picture owns the display: no header, no status bar, the controls beside it.
  const immersive = () => windowSize().height < ROOMY_MIN_HEIGHT && !platform.desktop;
  const live = () => session()?.phase() === "live";
  const fullBleed = () => immersive() && phase() === "live" && !!session() && session()!.phase() !== "lost";
  effect(() => {
    const centered = immersive();
    untracked(() => session()?.setAlign(centered ? "center" : "top"));
  });

  const readout = (): StreamReadout => {
    const s = session();
    const viewer = s?.viewerStats() ?? null;
    const stats = s?.stats() ?? null;
    const target = server();
    return {
      rttMs: s?.rtt() ?? null,
      inputMs: viewer?.inputMs ?? null,
      fps: viewer?.fps ?? 0,
      kbps: viewer?.kbps ?? 0,
      bridgeMs: viewer?.bridgeMs,
      presentMs: viewer?.presentMs,
      daemon: stats,
      viaPath: null,
      directHint: target ? directHint(target, stats?.path) : null,
    };
  };
  // Slowness counts only when it lasts, so one spike never lights the
  // indicator; it is said once per connection, briefly, and stays in the sheet.
  effect(() => {
    const s = session();
    const rtt = s?.rtt() ?? null;
    untracked(() => {
      const sample = slowWatch.sample(rtt === null ? null : readout());
      slow.set(sample.slow);
      if (sample.say) {
        clearNote();
        slowNote.set(true);
        noteTimer = setTimeout(() => slowNote.set(false), SLOW_NOTE_MS);
      }
    });
  });
  // What the chrome shows, as values that change only when what they say does: the
  // polls and every pong replace the objects they come from, and a slot that read
  // those would rebuild under the person's finger (an open menu would close).
  const problem = computed(() => (slow() ? connectionProblem(readout()) : null), { equals: (a, b) => a?.title === b?.title && a?.fix === b?.fix });
  const warning = computed(() => (phase() === "live" ? powerWarning(status()) : null), { equals: (a, b) => a?.label === b?.label && a?.action === b?.action });
  const tone = computed<IndicatorTone>(() => indicatorTone(!!problem(), warning()));
  const locked = computed(() => !!session()?.locked());
  const isLive = computed(() => phase() === "live" && live());
  const approved = computed(() => !!status()?.approved);
  const noteTitle = computed(() => (slowNote() ? (problem()?.title ?? "") : ""));
  const menuKind = computed(() => (phase() === "local" ? "local" : watching() && phase() === "live" ? "watch" : "full"));
  // A browser with a mouse types into the picture with its own keyboard: no button for one.
  const showKeyboard = computed(() => isLive() && !watching() && needsSoftKeyboard());
  const headerKind = computed(() => (!server() ? "none" : linuxDisplays() ? "linux" : "own"));

  const agentLabel = () => {
    const worker = watching()?.worker;
    return (worker ? screenAgents(status()).find((a) => a.worker === worker)?.name : undefined) || status()?.agent?.name || "An agent";
  };
  const takeOver = () => void connect("takeOver");
  const stopWatched = () => void stopAgent(watching()?.worker);
  const toggleMode = () => {
    const next = mode() === "touch" ? "trackpad" : "touch";
    mode.set(next);
    session()?.setMode(next);
  };

  // Only where phone use is on; it says who is using the phone, and opens the phone's own page.
  // Rebuilt only when it comes or goes: a status read while the menu is open never takes the item from under a finger.
  const phoneOn = computed(() => phoneAvailable() && !!phoneStatus()?.enabled);
  const phoneItem = () =>
    keyed(phoneOn, (on) =>
      <If when={on}><MenuItem testID="screen-phone" onSelect={() => props.nav.push({ name: "settings-phone" })} class={ITEM}>
          <Text class="text-body">Phone</Text>
          <Text class="text-sub text-muted-foreground">{phoneStatus()?.agent || "Ready"}</Text>
        </MenuItem></If>,
    );
  const keepAwakeItem = () =>
    keyed(approved, (on) =>
      <If when={on}><MenuItem disabled={busy()} onSelect={() => setKeepAwake(!status()?.keep_awake)} class={ITEM}>
          <Text class="text-body">{status()?.keep_awake ? "Let this computer sleep" : "Keep this computer awake"}</Text>
        </MenuItem></If>,
    );
  const optionsMenu = (testID: string) => (
    <Menu trigger={<View testID={testID} accessibilityLabel="Screen options" role="button" class="size-11 items-center justify-center rounded-full active:bg-white/15"><Icon name="more-horizontal" size={20} tone="onBlack" /></View>} popupClass={menuPopup()} backdropClass={MENU_BACKDROP}>
      <View class="px-4 pb-1 pt-2"><Text class="text-caption font-semibold text-muted-foreground line-clamp-1">{title()}</Text></View>
      {keyed(menuKind, (kind) =>
        <If when={kind === "local"} else={<If when={kind === "watch"} else={<>
            <MenuItem onSelect={toggleMode} class={ITEM}><Text class="text-body">{mode() === "touch" ? "Use as trackpad" : "Touch to click"}</Text></MenuItem>
            <MenuItem onSelect={() => session()?.fit()} class={ITEM}><Text class="text-body">Fit screen</Text></MenuItem>
            <If when={canShowFullSize()}><MenuItem testID="screen-full-size" onSelect={() => void session()?.fullSize(`Screen of ${untracked(title)}`)} class={ITEM}><Text class="text-body">Full size</Text></MenuItem></If>
            {keepAwakeItem()}
            {phoneItem()}
            {keyed(isLive, (on) =>
              <If when={on} else={<MenuItem testID="screen-menu-reconnect" onSelect={() => void connect()} class={ITEM}><Text class="text-body">Reconnect</Text></MenuItem>}><MenuItem onSelect={() => session()?.lose(null)} class={ITEM}><Text class="text-body">Disconnect</Text></MenuItem></If>,
            )}
          </>}><>
            <MenuItem onSelect={() => session()?.fit()} class={ITEM}><Text class="text-body">Fit screen</Text></MenuItem>
            <MenuItem testID="screen-stop-watching" onSelect={stopWatching} class={ITEM}><Text class="text-body">Stop watching</Text></MenuItem>
          </></If>}><>
            {keepAwakeItem()}
            {phoneItem()}
          </></If>,
      )}
    </Menu>
  );
  const indicator = () => keyed(isLive, (on) => (<If when={on}><ConnectionIndicator tone={tone()} locked={locked()} onPress={() => details.set(true)} /></If>));
  const keyboardButton = () =>
    keyed(showKeyboard, (on) =>
      <If when={on}><Button testID="screen-keyboard" accessibilityLabel={typing() ? "Hide keyboard" : "Show keyboard"} selected={typing()} onPress={() => typing.set(!typing())} class={`size-11 items-center justify-center rounded-full active:bg-white/15 ${typing() ? "bg-white/20" : ""}`}>
          <Icon name="keyboard" size={20} tone={typing() ? "onBlack" : "onBlackSoft"} />
        </Button></If>,
    );

  const iphoneAgent = () => (iphone()?.enabled ? iphone()!.active : undefined);
  const iphoneKey = computed(() => {
    const agent = iphoneAgent();
    return agent ? `${agent.worker}\n${agent.name ?? ""}` : "";
  });
  const iphoneRow = () => keyed(iphoneKey, (key) => (key ? iphoneCard() : null));
  const iphoneCard = () => {
    const agent = iphoneAgent();
    if (!agent) return null;
    const label = agent.name || "An agent";
    const who = agent.name || "the agent";
    return (
      <View testID="screen-iphone" class="w-full max-w-[440px] items-center gap-2 pt-2">
        <Text class="text-center text-body text-white">{`${label} is using your iPhone`}</Text>
        <View class="flex-row flex-wrap justify-center gap-2">
          <SmallButton testID="screen-iphone-watch" title="Watch" icon="eye" label={`Watch ${who} on your iPhone`} onPress={() => props.nav.push({ name: "settings-iphone" })} />
          <SmallButton testID="screen-iphone-done" title="Done" icon="check" label="Done: tell the agent you finished on your iPhone" disabled={iphoneBusy()} onPress={() => void changeIPhoneUse({ handoff_done: true })} />
          <SmallButton testID="screen-iphone-stop" title="Stop" label={`Stop ${who} on your iPhone`} destructive disabled={iphoneBusy()} onPress={() => void changeIPhoneUse({ stop_agent: true })} />
        </View>
      </View>
    );
  };

  type SetupKind = "virtual" | "unsupported" | "disabled" | "approve" | "permissions";
  const setupKind = (s: ScreenStatus): SetupKind =>
    !s.supported && s.platform === "linux" ? "virtual" : !s.supported ? "unsupported" : s.disabled ? "disabled" : !s.approved ? "approve" : "permissions";
  const view = computed(
    () => {
      const target = server();
      const s = status();
      if (!target) return { key: "none" };
      if (linuxDisplays()) return { key: `displays:${target.id}` };
      const p = phase();
      if (p === "setup" && s) return { key: `setup:${setupKind(s)}` };
      if (p === "live") return { key: `live:${sessions}:${session() ? "s" : ""}` };
      return { key: p };
    },
    { equals: (a, b) => a.key === b.key },
  );

  const body = (key: string): JSX.Element => {
    if (key === "none") return <EmptyState onBlack icon="desktop" title="No current server" action="Open Settings" onAction={() => props.nav.push({ name: "settings" })} />;
    if (key.startsWith("displays:")) return <AgentDisplays server={server()!} nav={props.nav} focus={props.worker} />;
    const p = phase();
    if (p === "checking" && !status()) return <EmptyState onBlack busy />;
    if (p === "checking") return <View class="flex-1 items-center justify-center"><Spinner size={28} tone="onBlackSoft" /></View>;
    if (p === "failed") return <EmptyState onBlack icon="cloud-offline" title="Disconnected" detail={reason()} action="Reconnect" onAction={() => void connect()} />;
    if (p === "setup")
      return (
        <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="grow items-center justify-center px-4 py-6">
          <Setup status={status()!} kind={setupKind(status()!)} name={title()} busy={busy()} reason={reason()} onEnable={() => void enable()} onOpenSettings={(id) => void openSettings(id)} onRestart={() => void restartMewla()} onCheck={() => void connect()} onChooseComputer={() => props.nav.push({ name: "settings" })} />
        </Scroll>
      );
    if (p === "agent") {
      return (
        <AgentCard agents={screenAgents(status())} busy={busy()} iphone={iphoneRow()} onWatch={(agent) => void connect("watch", agent.worker)} onStop={(agent) => void stopAgent(agent.worker)} onTakeOver={takeOver} />
      );
    }
    if (p === "local") return <LocalCard agents={screenAgents(status())} busy={busy()} iphone={iphoneRow()} onStop={(agent) => void stopAgent(agent.worker)} />;
    const current = session();
    if (!current) return <EmptyState onBlack busy />;
    return (
      <View class="flex-1">
        <View class="flex-1 flex-row">
          <View class="flex-1">
            <RemoteView session={current} label={`Screen of ${title()}`} modifiers={modifiersFor(status()?.platform)} typing={typing()} onTypingChange={(on) => typing.set(on)} />
            <If when={current.phase() === "connecting"}><View class="absolute inset-0 items-center justify-center"><Spinner size={28} tone="onBlackSoft" /></View></If>
            <If when={current.phase() === "lost"}><View testID="screen-lost" class="absolute inset-0 items-center justify-center gap-2 bg-black px-8">
                <Icon name={current.locked() ? "lock" : "cloud-offline"} size={36} tone="onBlack" />
                <Text class="text-center text-title font-semibold text-white">{current.locked() ? "Computer is locked" : "Disconnected"}</Text>
                <If when={!current.locked() && current.reason()}><Text class="text-center text-sub text-white/60 line-clamp-1">{current.reason()!}</Text></If>
                <PillButton testID="screen-reconnect" kind="white" title="Reconnect" onPress={() => void connect()} />
              </View></If>
          </View>
          <If when={fullBleed() && live()}><Scroll keyboardShouldPersistTaps="handled" testID="screen-rail" class="grow-0 bg-black" contentClass="grow items-center justify-center gap-1 p-1">
              {indicator()}
              <If when={watching()}><>
                  <RailButton icon="hand" label="Take over" disabled={busy()} onPress={takeOver} />
                  <RailButton icon="stop" label={`Stop ${agentLabel()}`} tone="seal" disabled={busy()} onPress={stopWatched} />
                </></If>
              {keyboardButton()}
              {optionsMenu("screen-rail-options")}
            </Scroll></If>
        </View>
        <If when={watching() && live() && !immersive()}><View testID="screen-watch-bar" class="min-h-[52px] flex-row items-center gap-2 bg-black px-4 py-1.5">
            <View class="size-2 rounded-full bg-white" />
            <Text class="flex-1 text-sub text-white/70 line-clamp-1">{`Watching ${agentLabel()}`}</Text>
            <SmallButton testID="watch-take-over" title="Take over" icon="hand" filled disabled={busy()} onPress={takeOver} />
            <SmallButton title="Stop" label={`Stop ${agentLabel()}`} destructive disabled={busy()} onPress={stopWatched} />
          </View></If>
      </View>
    );
  };

  // The top bar's end: the slow note, the connection dot, the keyboard and ⋯, in white on black.
  const headerRight = () => (
    <View class="flex-row items-center gap-1">
      {keyed(headerKind, (kind) =>
      <If when={kind === "own"} else={<If when={kind === "linux" && platform.os === "android"}><Menu trigger={<View testID="screen-options" accessibilityLabel="Screen options" role="button" class="size-11 items-center justify-center rounded-full" style={{ display: phoneOn() ? "flex" : "none", flexDirection: "column" }}><Icon name="more-horizontal" size={20} tone="onBlack" /></View>} popupClass={menuPopup()} backdropClass={MENU_BACKDROP}>
          {phoneItem()}
        </Menu></If>}><>
          {keyed(noteTitle, (note) => (<If when={note}>{(note) => <Text testID="screen-slow-note" class="text-caption text-white/70">{note()}</Text>}</If>))}
          {indicator()}
          {keyboardButton()}
          {optionsMenu("screen-options")}
        </></If>,
          )}
    </View>
  );

  return (
    <NavPage
      title={linuxDisplays() ? "Screen" : title()}
      testID="screen-page"
      column="full"
      headerVisible={!fullBleed()}
      // Black in both themes, like the picture viewer (SPEC §15 Full-screen remote).
      headerClass="bg-black text-white tint-white"
      header={<If when={!mobile}><BlackHeader title={linuxDisplays() ? "Screen" : title()} right={headerRight()} /></If>}
      statusBar={{ hidden: fullBleed(), foreground: "light" }}
      headerRight={mobile ? headerRight() : undefined}
    >
      <View class="flex-1 bg-black">{((key) => untracked(() => body(key)))(view().key)}</View>
      <ConnectionSheet
        open={details()}
        onClose={() => details.set(false)}
        problem={problem()}
        rows={diagnosticRows(readout(), powerSummary(status()?.power, status()?.keep_awake ?? false))}
        warning={warning()}
        locked={locked()}
        busy={busy()}
        onKeepAwake={() => setKeepAwake(true)}
      />
    </NavPage>
  );
}

/**
 * A slot that is rebuilt only when `key` says something new; call it inside JSX.
 * What `render` reads is read once, unless it reads it inside its own JSX.
 */
function keyed<K>(key: () => K, render: (key: K) => JSX.Element): JSX.Element {
  return ((value: K) => untracked(() => render(value)))(key());
}

/**
 * The only standing trace of the connection: a dot that is nearly invisible
 * while all is well, amber when something needs attention, a lock when the
 * computer is locked. It sits in chrome, never on the picture; a tap opens
 * the details.
 */
function ConnectionIndicator(props: { tone: IndicatorTone; locked: boolean; onPress: () => void }) {
  return (
    <Button
      testID="screen-indicator"
      accessibilityLabel={props.locked ? "Computer is locked, connection details" : props.tone === "warn" ? "Connection needs attention" : "Connection details"}
      onPress={props.onPress}
      class="size-11 items-center justify-center"
    >
      <If when={props.locked} else={<View class={`size-2 rounded-full ${props.tone === "warn" ? "bg-white" : "bg-white/30"}`} />}><Icon name="lock" size={14} tone="onBlack" /></If>
    </Button>
  );
}

/**
 * What stands behind the indicator: the problem and its way out, if there is
 * one, then every diagnostic once. None of it is ever drawn on the picture.
 */
function ConnectionSheet(props: {
  open: boolean;
  onClose: () => void;
  problem: ReturnType<typeof connectionProblem>;
  rows: ReturnType<typeof diagnosticRows>;
  warning: ReturnType<typeof powerWarning>;
  locked: boolean;
  busy: boolean;
  onKeepAwake: () => void;
}) {
  return (
    <Sheet testID="screen-connection" open={props.open} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe popupClass="max-h-[88%] rounded-t-(--radius-sheet) bg-sheet px-4 pb-8 pt-4 shadow-float" backdropClass="bg-scrim">
      <View class="gap-4 px-2">
        <Text class="text-title font-bold" role="header">Connection</Text>
        <If when={props.problem}>{(problem) => <View class="items-start gap-1">
            <Text class="text-sub font-semibold text-warning">{problem().title}</Text>
            <If when={problem().fix}>{(fix) => <Text class="text-sub text-muted-foreground">{fix()}</Text>}</If>
          </View>}</If>
        <If when={props.locked}><Text class="text-sub font-semibold text-muted-foreground">Computer is locked</Text></If>
        <If when={props.warning}>{(warningValue) => <View class="items-start gap-1">
            <Text class={`text-sub font-semibold ${warningValue().label === "On battery" ? "text-muted-foreground" : "text-warning"}`}>{warningValue().action ? "This computer may sleep" : warningValue().label}</Text>
            <If when={warningValue().action}><PillButton testID="screen-keep-awake" title="Keep awake" busy={props.busy} onPress={props.onKeepAwake} /></If>
          </View>}</If>
        <View class="gap-2">
          {props.rows.map((row) => (
            <View class="flex-row justify-between gap-4">
              <Text class="text-body text-muted-foreground">{row.label}</Text>
              <Text class="shrink text-right text-body" style={{ fontVariant: ["tabular-nums"] }}>{row.value}</Text>
            </View>
          ))}
        </View>
      </View>
    </Sheet>
  );
}

/**
 * A small pill on the remote's black: translucent white, or solid white when it is the one step (Take over).
 * `onCard` inside a surface group (a row's Stop, Open settings): the Soft ghost pill.
 */
function SmallButton(props: { title: string; icon?: IconName; label?: string; filled?: boolean; destructive?: boolean; disabled?: boolean; onCard?: boolean; testID?: string; onPress: () => void }) {
  const fill = () => (props.onCard ? "bg-muted" : props.filled ? "bg-white" : "bg-white/15");
  const ink = () => (props.onCard ? "" : props.filled ? "text-black" : "text-white");
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.label}
      disabled={props.disabled}
      onPress={props.onPress}
      hitSlop={4}
      class={`h-9 flex-row items-center gap-1.5 rounded-full px-3.5 active:opacity-70 disabled:opacity-40 ${fill()}`}
    >
      <If when={props.icon}>{(icon) => <Icon name={icon()} size={14} tone={props.onCard ? "soft" : props.filled ? "onBlackSoft" : "onBlack"} />}</If>
      <Text class={`text-sub font-semibold ${ink()}`}>{props.title}</Text>
    </Button>
  );
}

/** A control in the sideways rail beside the picture: a translucent white key. */
function RailButton(props: { icon: IconName; label: string; tone?: "seal"; disabled?: boolean; onPress: () => void }) {
  return (
    <Button accessibilityLabel={props.label} disabled={props.disabled} onPress={props.onPress} class="size-11 items-center justify-center rounded-xl bg-white/15 active:bg-white/25 disabled:opacity-40">
      <Icon name={props.icon} size={18} tone={props.tone ?? "onBlack"} />
    </Button>
  );
}

/** The desktops' and the web's top bar on black: Back, the computer, the controls (a phone's system header is made black instead). */
function BlackHeader(props: { title: string; right: JSX.Element }) {
  const stack = PageStack();
  return (
    <View testID="page-header" class="h-[52px] flex-row items-center gap-2 bg-black px-4">
      <If when={stack.canGoBack()}><Button testID="page-back" accessibilityLabel="Back" onPress={stack.back} class="size-10 items-center justify-center rounded-full hover:bg-white/15">
          <Icon name="chevron-left" size={20} tone="onBlack" />
        </Button></If>
      <Text role="header" class="min-w-0 flex-1 text-title font-bold text-white line-clamp-1">{props.title}</Text>
      {props.right}
    </View>
  );
}

/** Which agents, by worker and name: the rows are rebuilt when this changes, not on every poll. */
function agentsKey(agents: () => ScreenAgent[]): () => string {
  return computed(() => agents().map((a) => `${a.worker}\n${a.name ?? ""}`).join("\n\n"));
}

function AgentCard(props: { agents: ScreenAgent[]; busy: boolean; iphone: JSX.Element; onWatch: (agent: ScreenAgent) => void; onStop: (agent: ScreenAgent) => void; onTakeOver: () => void }) {
  const label = (agent: ScreenAgent | undefined) => agent?.name || "An agent";
  const which = agentsKey(() => props.agents);
  return (
    <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="grow items-center justify-center gap-3 px-6 py-6">
      <View testID="screen-agents" class="w-full items-center gap-3">
        <Icon name="desktop" size={36} tone="onBlackSoft" />
        <Text class="text-center text-title font-semibold text-white">{props.agents.length > 1 ? `${props.agents.length} agents are using the screen` : `${label(props.agents[0])} is using the screen`}</Text>
        {keyed(which, () =>
          props.agents.map((agent) => (
            <View class="flex-row flex-wrap items-center justify-center gap-2">
              <If when={props.agents.length > 1}><Text class="text-center text-body text-white">{label(agent)}</Text></If>
              <SmallButton title="Watch" icon="eye" label={`Watch ${label(agent)}`} disabled={props.busy} onPress={() => props.onWatch(agent)} />
              <SmallButton title="Stop" label={`Stop ${label(agent)}`} destructive disabled={props.busy} onPress={() => props.onStop(agent)} />
            </View>
          )),
        )}
        {props.iphone}
        <View class="pt-2">
          <SmallButton testID="take-over" title={props.agents.length > 1 ? "Take over (stops all)" : "Take over"} icon="hand" filled disabled={props.busy} onPress={props.onTakeOver} />
        </View>
      </View>
    </Scroll>
  );
}

/** This device is the computer: a live view would only mirror itself. */
function LocalCard(props: { agents: ScreenAgent[]; busy: boolean; iphone: JSX.Element; onStop: (agent: ScreenAgent) => void }) {
  const which = agentsKey(() => props.agents);
  return (
    <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="grow items-center justify-center px-4 py-6">
      <View testID="screen-local" class="w-full max-w-[440px]">
        <Section>
          {keyed(which, () =>
            <If when={props.agents.length} else={<Row lead={tile("desktop")} title="No agent on this screen" />}>{props.agents.map((agent) => (
                <Row lead={tile("desktop")} title={agent.name || "An agent"} trailing={<SmallButton onCard title="Stop" label={`Stop ${agent.name || "An agent"}`} destructive disabled={props.busy} onPress={() => props.onStop(agent)} />} />
              ))}</If>,
          )}
        </Section>
      </View>
      {props.iphone}
    </Scroll>
  );
}

/**
 * The one next step before a viewer can open, the same on every platform:
 * unsupported, turned off at the computer, not yet allowed, or waiting for
 * operating-system permissions. It follows the computer while it is shown.
 */
function Setup(props: {
  status: ScreenStatus;
  kind: "virtual" | "unsupported" | "disabled" | "approve" | "permissions";
  name: string;
  busy: boolean;
  reason: string;
  onEnable: () => void;
  onOpenSettings: (permission: string) => void;
  onRestart: () => void;
  onCheck: () => void;
  onChooseComputer: () => void;
}) {
  // No screen to share, but its agents' browsers can have one: the switch is the way in.
  if (props.kind === "virtual") return <View class="w-full max-w-[440px]"><VirtualScreenSwitch /></View>;
  if (props.kind === "unsupported") {
    // A browser cannot choose another computer: it is served by this one.
    const unsupported = () => unsupportedScreen(props.status, platform.os !== "web");
    return <EmptyState onBlack icon="desktop" title={unsupported().title} detail={unsupported().detail ?? undefined} action={unsupported().canChooseComputer ? "Choose a computer" : undefined} onAction={props.onChooseComputer} />;
  }
  if (props.kind === "disabled") return <EmptyState onBlack icon="lock" title="Screen is off" detail={`Run mewla screen enable on ${props.name}.`} action="Check again" onAction={props.onCheck} />;
  if (props.kind === "approve") {
    return (
      <View class="items-center gap-2 px-6 py-16" testID="screen-enable">
        <Icon name="desktop" size={28} tone="onBlack" />
        <If when={props.busy}><Text class="text-center text-title font-semibold text-white">{`Waiting for Allow on ${props.name}…`}</Text></If>
        <If when={!props.busy && props.reason}><Text class="text-center text-sub text-white/60">{props.reason}</Text></If>
        <PillButton testID="screen-turn-on" kind="white" title={props.busy ? "Waiting…" : "Turn on Screen"} busy={props.busy} onPress={props.onEnable} />
      </View>
    );
  }
  const missing = () => (props.status.permissions ?? []).filter((p) => !p.granted);
  // Some grants apply only to a new Mewla process on the computer.
  const restart = computed(() => missing().some((p) => p.restart_required) && !!props.status.can_restart);
  const rows = computed(() => missing().map((p) => [p.id, p.name, p.guidance ?? "", p.can_open ? 1 : 0, p.restart_required ? 1 : 0].join("\n")).join("\n\n"));
  return (
    <View class="w-full max-w-[440px]" testID="screen-permissions">
      <View class="items-center gap-2 py-6">
        <Icon name="shield-check" size={28} tone="onBlack" />
        <Text class="text-center text-title font-semibold text-white">{`Allow Screen on ${props.name}`}</Text>
        <If when={!restart()}><PillButton testID="screen-ask" kind="white" title={`Ask on ${props.name}`} busy={props.busy} onPress={props.onEnable} /></If>
      </View>
      {keyed(rows, () =>
        <If when={missing().length}><Section>
            {missing().map((p) => (
              <Row
                title={p.name}
                subtitle={p.guidance}
                trailing={<If when={p.can_open && !p.restart_required}><SmallButton onCard title="Open settings" onPress={() => props.onOpenSettings(p.id)} /></If>}
              />
            ))}
          </Section></If>,
      )}
      <View class="mt-4 items-center">
        {keyed(restart, (on) =>
          <If when={on} else={<PillButton testID="screen-check" kind="white" title="Check again" onPress={props.onCheck} />}><PillButton testID="screen-restart" kind="white" title={props.busy ? "Restarting Mewla…" : "Restart Mewla"} busy={props.busy} onPress={props.onRestart} /></If>,
        )}
        <If when={props.reason}>{(reason) => <Text class="mt-2 text-center text-sub text-white/70 line-clamp-2">{reason()}</Text>}</If>
      </View>
    </View>
  );
}
