import { cleanup, computed, effect, platform, Scroll, signal, Text, untracked, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { Button, Menu, MenuItem } from "nsh/base";
import { GuardedSheet as Sheet } from "../../shell/AppSheet";
import { NavPage } from "../../shell/NavPage";
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
import { EmptyState, PillButton, Row, Section, Spinner } from "../kit/kit";
import { Icon, type IconName } from "../kit/Icon";
import { inFront } from "../kit/inFront";
import type { PageNav } from "../routes";
import { AgentDisplays } from "./Displays";
import { modifiersFor, needsSoftKeyboard, RemoteSession, RemoteView } from "./RemoteView";
import { VirtualScreenSwitch, virtualScreenOn } from "./VirtualScreen";

/** Window height (pt) below which a page header does not fit beside the picture: a phone held sideways. */
const ROOMY_MIN_HEIGHT = 500;

type Phase = "checking" | "failed" | "setup" | "agent" | "local" | "live";
type How = "auto" | "takeOver" | "watch";

const MENU_POPUP = "rounded-2xl bg-popover p-2";
const MENU_BACKDROP = "bg-black/30";
const ITEM = "rounded-lg px-4 py-3 active:bg-accent disabled:opacity-50";

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
      on ? (
        <MenuItem testID="screen-phone" onSelect={() => props.nav.push({ name: "settings-phone" })} class={ITEM}>
          <Text class="text-base">Phone</Text>
          <Text class="text-sm text-muted-foreground">{phoneStatus()?.agent || "Ready"}</Text>
        </MenuItem>
      ) : null,
    );
  const keepAwakeItem = () =>
    keyed(approved, (on) =>
      on ? (
        <MenuItem disabled={busy()} onSelect={() => setKeepAwake(!status()?.keep_awake)} class={ITEM}>
          <Text class="text-base">{status()?.keep_awake ? "Let this computer sleep" : "Keep this computer awake"}</Text>
        </MenuItem>
      ) : null,
    );
  const optionsMenu = (testID: string) => (
    <Menu trigger={<View testID={testID} accessibilityLabel="Screen options" role="button" class="p-2"><Icon name="more-horizontal" size={20} /></View>} popupClass={MENU_POPUP} backdropClass={MENU_BACKDROP}>
      <View class="px-4 pb-1 pt-2"><Text class="text-sm font-medium text-muted-foreground line-clamp-1">{title()}</Text></View>
      {keyed(menuKind, (kind) =>
        kind === "local" ? (
          <>
            {keepAwakeItem()}
            {phoneItem()}
          </>
        ) : kind === "watch" ? (
          <>
            <MenuItem onSelect={() => session()?.fit()} class={ITEM}><Text class="text-base">Fit screen</Text></MenuItem>
            <MenuItem testID="screen-stop-watching" onSelect={stopWatching} class={ITEM}><Text class="text-base">Stop watching</Text></MenuItem>
          </>
        ) : (
          <>
            <MenuItem onSelect={toggleMode} class={ITEM}><Text class="text-base">{mode() === "touch" ? "Use as trackpad" : "Touch to click"}</Text></MenuItem>
            <MenuItem onSelect={() => session()?.fit()} class={ITEM}><Text class="text-base">Fit screen</Text></MenuItem>
            {keepAwakeItem()}
            {phoneItem()}
            {keyed(isLive, (on) =>
              on ? (
                <MenuItem onSelect={() => session()?.lose(null)} class={ITEM}><Text class="text-base">Disconnect</Text></MenuItem>
              ) : (
                <MenuItem testID="screen-menu-reconnect" onSelect={() => void connect()} class={ITEM}><Text class="text-base">Reconnect</Text></MenuItem>
              ),
            )}
          </>
        ),
      )}
    </Menu>
  );
  const indicator = () => keyed(isLive, (on) => (on ? <ConnectionIndicator tone={tone()} locked={locked()} onPress={() => details.set(true)} /> : null));
  const keyboardButton = () =>
    keyed(showKeyboard, (on) =>
      on ? (
        <Button testID="screen-keyboard" accessibilityLabel={typing() ? "Hide keyboard" : "Show keyboard"} selected={typing()} onPress={() => typing.set(!typing())} class={`rounded-full p-2 ${typing() ? "bg-secondary" : ""}`}>
          <Icon name="keyboard" size={20} tone={typing() ? "ink" : "soft"} />
        </Button>
      ) : null,
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
        <Text class="text-center text-base">{`${label} is using your iPhone`}</Text>
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
    if (key === "none") return <EmptyState icon="desktop" title="No current server" action="Open Settings" onAction={() => props.nav.push({ name: "settings" })} />;
    if (key.startsWith("displays:")) return <AgentDisplays server={server()!} nav={props.nav} focus={props.worker} />;
    const p = phase();
    if (p === "checking" && !status()) return <EmptyState busy />;
    if (p === "checking") return <View class="flex-1 items-center justify-center"><Spinner size={28} /></View>;
    if (p === "failed") return <EmptyState icon="cloud-offline" title="Disconnected" detail={reason()} action="Reconnect" onAction={() => void connect()} />;
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
    if (!current) return <EmptyState busy />;
    return (
      <View class="flex-1">
        <View class="flex-1 flex-row">
          <View class="flex-1">
            <RemoteView session={current} label={`Screen of ${title()}`} modifiers={modifiersFor(status()?.platform)} typing={typing()} onTypingChange={(on) => typing.set(on)} />
            {current.phase() === "connecting" ? <View class="absolute inset-0 items-center justify-center"><Spinner size={28} /></View> : null}
            {current.phase() === "lost" ? (
              <View testID="screen-lost" class="absolute inset-0 items-center justify-center gap-2 bg-background px-8">
                <Icon name={current.locked() ? "lock" : "cloud-offline"} size={36} />
                <Text class="text-center text-lg font-semibold">{current.locked() ? "Computer is locked" : "Disconnected"}</Text>
                {!current.locked() && current.reason() ? <Text class="text-center text-sm text-muted-foreground line-clamp-1">{current.reason()!}</Text> : null}
                <PillButton testID="screen-reconnect" filled title="Reconnect" onPress={() => void connect()} />
              </View>
            ) : null}
          </View>
          {fullBleed() && live() ? (
            // Sideways, the controls stand beside the picture, which is wider
            // than tall and leaves that strip unused.
            <Scroll keyboardShouldPersistTaps="handled" testID="screen-rail" class="grow-0 border-l border-border bg-background" contentClass="grow items-center justify-center gap-1 p-1">
              {indicator()}
              {watching() ? (
                <>
                  <RailButton icon="hand" label="Take over" disabled={busy()} onPress={takeOver} />
                  <RailButton icon="stop" label={`Stop ${agentLabel()}`} tone="seal" disabled={busy()} onPress={stopWatched} />
                </>
              ) : null}
              {keyboardButton()}
              {optionsMenu("screen-rail-options")}
            </Scroll>
          ) : null}
        </View>
        {watching() && live() && !immersive() ? (
          <View testID="screen-watch-bar" class="flex-row items-center gap-2 border-t border-border px-4 py-1.5">
            <Icon name="eye" size={16} />
            <Text class="flex-1 text-sm text-muted-foreground line-clamp-1">{`Watching ${agentLabel()}`}</Text>
            <SmallButton testID="watch-take-over" title="Take over" icon="hand" filled disabled={busy()} onPress={takeOver} />
            <SmallButton title="Stop" label={`Stop ${agentLabel()}`} destructive disabled={busy()} onPress={stopWatched} />
          </View>
        ) : null}
      </View>
    );
  };

  return (
    <NavPage
      title={linuxDisplays() ? "Screen" : title()}
      testID="screen-page"
      column="full"
      headerVisible={!fullBleed()}
      statusBar={{ hidden: fullBleed() }}
      headerRight={
        <View class="flex-row items-center gap-1">
          {keyed(headerKind, (kind) =>
            kind === "own" ? (
              <>
                {keyed(noteTitle, (note) => (note ? <Text testID="screen-slow-note" class="text-xs text-warning">{note}</Text> : null))}
                {indicator()}
                {keyboardButton()}
                {optionsMenu("screen-options")}
              </>
            ) : kind === "linux" && platform.os === "android" ? (
              // Linux: the menu holds only the rows of other surfaces (the Phone); with none, its button is hidden
              // (it opened an empty sheet). The menu itself stays: one made later in the header does not open.
              <Menu trigger={<View testID="screen-options" accessibilityLabel="Screen options" role="button" class="p-2" style={{ display: phoneOn() ? "flex" : "none" }}><Icon name="more-horizontal" size={20} /></View>} popupClass={MENU_POPUP} backdropClass={MENU_BACKDROP}>
                {phoneItem()}
              </Menu>
            ) : null,
          )}
        </View>
      }
    >
      <View class="flex-1 bg-background">{((key) => untracked(() => body(key)))(view().key)}</View>
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
      class="size-10 items-center justify-center"
    >
      {props.locked ? <Icon name="lock" size={14} /> : <View class={`size-2 rounded-full ${props.tone === "warn" ? "bg-warning" : "bg-muted-foreground opacity-40"}`} />}
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
    <Sheet testID="screen-connection" open={props.open} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe popupClass="max-h-[85%] rounded-t-3xl bg-card px-6 pb-8 pt-4" backdropClass="bg-black/30">
      <View class="gap-4">
        <Text class="text-lg font-semibold" role="header">Connection</Text>
        {props.problem ? (
          <View class="items-start gap-1">
            <Text class="text-sm font-medium text-warning">{props.problem.title}</Text>
            {props.problem.fix ? <Text class="text-sm text-muted-foreground">{props.problem.fix}</Text> : null}
          </View>
        ) : null}
        {props.locked ? <Text class="text-sm font-medium text-muted-foreground">Computer is locked</Text> : null}
        {props.warning ? (
          <View class="items-start gap-1">
            <Text class={`text-sm font-medium ${props.warning.label === "On battery" ? "text-muted-foreground" : "text-warning"}`}>{props.warning.action ? "This computer may sleep" : props.warning.label}</Text>
            {props.warning.action ? <PillButton testID="screen-keep-awake" title="Keep awake" busy={props.busy} onPress={props.onKeepAwake} /> : null}
          </View>
        ) : null}
        <View class="gap-2">
          {props.rows.map((row) => (
            <View class="flex-row justify-between gap-4">
              <Text class="text-base text-muted-foreground">{row.label}</Text>
              <Text class="shrink text-right text-base" style={{ fontVariant: ["tabular-nums"] }}>{row.value}</Text>
            </View>
          ))}
        </View>
      </View>
    </Sheet>
  );
}

function SmallButton(props: { title: string; icon?: IconName; label?: string; filled?: boolean; destructive?: boolean; disabled?: boolean; testID?: string; onPress: () => void }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.label}
      disabled={props.disabled}
      onPress={props.onPress}
      class={`flex-row items-center gap-1.5 rounded-full px-3 py-1.5 active:opacity-70 disabled:opacity-50 ${props.filled ? "bg-primary" : "bg-secondary"}`}
    >
      {props.icon ? <Icon name={props.icon} size={14} tone={props.filled ? "paper" : "soft"} /> : null}
      <Text class={`text-sm font-medium ${props.filled ? "text-primary-foreground" : props.destructive ? "text-destructive" : ""}`}>{props.title}</Text>
    </Button>
  );
}

function RailButton(props: { icon: IconName; label: string; tone?: "seal"; disabled?: boolean; onPress: () => void }) {
  return (
    <Button accessibilityLabel={props.label} disabled={props.disabled} onPress={props.onPress} class="size-11 items-center justify-center rounded-lg border border-border bg-card active:bg-accent disabled:opacity-50">
      <Icon name={props.icon} size={18} tone={props.tone} />
    </Button>
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
        <Icon name="desktop" size={36} tone="faint" />
        <Text class="text-center text-lg font-semibold">{props.agents.length > 1 ? `${props.agents.length} agents are using the screen` : `${label(props.agents[0])} is using the screen`}</Text>
        {keyed(which, () =>
          props.agents.map((agent) => (
            <View class="flex-row flex-wrap items-center justify-center gap-2">
              {props.agents.length > 1 ? <Text class="text-center text-base">{label(agent)}</Text> : null}
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
            props.agents.length ? (
              props.agents.map((agent) => (
                <Row icon="desktop" title={agent.name || "An agent"} trailing={<SmallButton title="Stop" label={`Stop ${agent.name || "An agent"}`} destructive disabled={props.busy} onPress={() => props.onStop(agent)} />} />
              ))
            ) : (
              <Row icon="desktop" title="No agent on this screen" />
            ),
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
    return <EmptyState icon="desktop" title={unsupported().title} detail={unsupported().detail ?? undefined} action={unsupported().canChooseComputer ? "Choose a computer" : undefined} onAction={props.onChooseComputer} />;
  }
  if (props.kind === "disabled") return <EmptyState icon="lock" title="Screen is off" detail={`Run mewla screen enable on ${props.name}.`} action="Check again" onAction={props.onCheck} />;
  if (props.kind === "approve") {
    return (
      <View class="items-center gap-2 px-6 py-16" testID="screen-enable">
        <Icon name="desktop" size={28} />
        {props.busy ? <Text class="text-center text-lg font-semibold">{`Waiting for Allow on ${props.name}…`}</Text> : null}
        {!props.busy && props.reason ? <Text class="text-center text-sm text-muted-foreground">{props.reason}</Text> : null}
        <PillButton testID="screen-turn-on" filled title={props.busy ? "Waiting…" : "Turn on Screen"} busy={props.busy} onPress={props.onEnable} />
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
        <Icon name="shield-check" size={28} />
        <Text class="text-center text-lg font-semibold">{`Allow Screen on ${props.name}`}</Text>
        {!restart() ? <PillButton testID="screen-ask" filled title={`Ask on ${props.name}`} busy={props.busy} onPress={props.onEnable} /> : null}
      </View>
      {keyed(rows, () =>
        missing().length ? (
          <Section>
            {missing().map((p) => (
              <Row
                title={p.name}
                subtitle={p.guidance}
                trailing={p.can_open && !p.restart_required ? <SmallButton title="Open settings" onPress={() => props.onOpenSettings(p.id)} /> : undefined}
              />
            ))}
          </Section>
        ) : null,
      )}
      <View class="mt-4 items-center">
        {keyed(restart, (on) =>
          on ? (
            <PillButton testID="screen-restart" filled title={props.busy ? "Restarting Mewla…" : "Restart Mewla"} busy={props.busy} onPress={props.onRestart} />
          ) : (
            <PillButton testID="screen-check" title="Check again" onPress={props.onCheck} />
          ),
        )}
        {props.reason ? <Text class="mt-2 text-center text-sm text-destructive line-clamp-2">{props.reason}</Text> : null}
      </View>
    </View>
  );
}
