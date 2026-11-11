import { computed, effect, lifecycle, signal, Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { Button, Menu, MenuItem } from "nsh/base";
import { NavPage } from "../../shell/NavPage";
import { displayRoute } from "../../../core/screen/displayRoute";
import { haltDisplay, listDisplays, resumeDisplay, screenRequest, type AgentDisplay, type ScreenAction, type ScreenAgent, type ScreenStatus } from "../../../core/screen/screen";
import { screenAgents, screenEntry, unsupportedScreen } from "../../../core/screen/screenPageModel";
import { failureText } from "../../../core/more/daemon";
import type { StoredServer } from "../../../core/storedServerContract";
import { daemon } from "../daemon";
import { EmptyState, Notice, Row, Section, Spinner, SwitchRow } from "../kit/kit";
import { Icon } from "../kit/Icon";
import type { PageNav } from "../routes";
import { modifiersFor, RemoteSession, RemoteView } from "./RemoteView";
import { VirtualScreenSwitch } from "./VirtualScreen";

/**
 * Screen: the current computer's own screen (Mac, Windows), or on Linux each
 * agent's display. Exactly one computer: a switch drops everything and reads the next.
 */
export function ScreenPage(props: { nav: PageNav }) {
  const status = signal<ScreenStatus | null>(null);
  const error = signal("");
  const busy = signal(false);
  const how = signal<{ mode: "auto" | "takeOver" | "watch"; worker?: string }>({ mode: "auto" });
  const session = signal<RemoteSession | null>(null);
  const typing = signal(false);
  const mode = signal<"touch" | "trackpad">("touch");
  let serverId = "";

  const server = (): StoredServer | null => daemon()?.server ?? null;
  const name = () => status()?.name || server()?.name || "this computer";

  const read = async () => {
    const target = server();
    if (!target) return;
    try {
      const next = await screenRequest(target);
      if (server()?.id !== target.id) return;
      status.set(next);
      error.set("");
    } catch (failure) {
      if (server()?.id === target.id) error.set(failureText(failure, "Screen request failed"));
    }
  };
  const act = async (action: ScreenAction) => {
    const target = server();
    if (!target || busy()) return;
    busy.set(true);
    try {
      const next = await screenRequest(target, action);
      if (server()?.id === target.id) {
        status.set(next);
        error.set("");
      }
    } catch (failure) {
      if (server()?.id === target.id) error.set(failureText(failure, "Screen request failed"));
    } finally {
      busy.set(false);
    }
  };

  // The computer: a switch drops what the last one said.
  effect(() => {
    const target = server();
    untracked(() => {
      if ((target?.id ?? "") === serverId) return;
      serverId = target?.id ?? "";
      session()?.disconnect();
      session.set(null);
      status.set(null);
      error.set("");
      how.set({ mode: "auto" });
      if (target) void read();
    });
  });

  const linuxDisplays = () => status()?.platform === "linux" && !!status()?.agent_displays;
  const entry = () => {
    const s = status();
    if (!s) return null;
    if (linuxDisplays()) return "displays";
    return screenEntry(s, how().mode);
  };

  // The own screen's viewer, made once per way of looking (take over, watch one agent).
  effect(() => {
    const e = entry();
    const target = server();
    const look = how();
    untracked(() => {
      const current = session();
      if (e !== "live" || !target) {
        if (current) {
          current.disconnect();
          session.set(null);
        }
        return;
      }
      if (current) return;
      const next = new RemoteSession(target, { watch: look.mode === "watch", worker: look.worker, align: "center" });
      next.mode = mode();
      session.set(next);
      next.connect();
    });
  });

  // Asks again while waiting: for permissions, for an agent to finish; slowly while live.
  effect(() => {
    const e = entry();
    if (!e || e === "displays") return;
    const timer = setInterval(() => void read(), e === "live" ? 30_000 : 3_000);
    return () => clearInterval(timer);
  });

  // Away from the app: no picture streams. Back: it reconnects.
  effect(() => {
    const active = lifecycle() === "active";
    untracked(() => {
      const current = session();
      if (!current) return;
      if (!active) current.lose(null);
      else if (current.phase() === "lost") current.connect();
    });
  });

  const takeOver = () => {
    session()?.disconnect();
    session.set(null);
    how.set({ mode: "takeOver" });
  };
  const watch = (agent: ScreenAgent) => {
    session()?.disconnect();
    session.set(null);
    how.set({ mode: "watch", worker: agent.worker });
  };
  const stop = async (agent?: ScreenAgent) => {
    await act({ action: "stop_agent", worker: agent?.worker });
    session()?.disconnect();
    session.set(null);
    how.set({ mode: "auto" });
  };

  const view = computed(() => ({ key: `${entry()}:${session() ? "s" : ""}` }), { equals: (a, b) => a.key === b.key });

  const body = (): JSX.Element => {
    const e = entry();
    const s = status();
    if (!server()) return <EmptyState icon="desktop" title="No current server" action="Open Settings" onAction={() => props.nav.push({ name: "settings" })} />;
    if (!s) return error() ? <EmptyState icon="cloud-offline" title="Disconnected" detail={error()} action="Reconnect" onAction={() => void read()} /> : <EmptyState busy />;
    if (e === "displays") return <AgentDisplays server={server()!} nav={props.nav} />;
    if (e === "setup") return <Setup status={s} name={name()} busy={busy()} reason={error() || null} onAct={(action) => void act(action)} onCheck={() => void read()} onChooseComputer={() => props.nav.push({ name: "settings" })} />;
    if (e === "local") return <LocalCard status={s} onStop={(agent) => void stop(agent)} />;
    if (e === "agent") return <AgentCard status={s} onWatch={watch} onStop={(agent) => void stop(agent)} onTakeOver={takeOver} />;
    const current = session();
    return current ? <Live session={current} status={s} watching={how().mode === "watch"} typing={typing()} onTyping={(on) => typing.set(on)} onTakeOver={takeOver} onStop={() => void stop(screenAgents(s).find((a) => a.worker === how().worker))} /> : <EmptyState busy />;
  };

  return (
    <NavPage
      title="Screen"
      testID="screen-page"
      column="full"
      headerRight={
        <View class="flex-row items-center gap-1">
          {session() && session()!.locked() ? <Icon name="lock" size={18} tone="warning" /> : null}
          {session() && session()!.phase() === "live" && how().mode !== "watch" ? (
            <Button testID="screen-keyboard" accessibilityLabel={typing() ? "Hide keyboard" : "Show keyboard"} onPress={() => typing.set(!typing())} class="p-2">
              <Icon name="keyboard" size={20} />
            </Button>
          ) : null}
          {session() ? (
            <Menu trigger={<View testID="screen-options" accessibilityLabel="Screen options" class="p-2"><Icon name="more-horizontal" size={20} /></View>} popupClass="rounded-2xl bg-popover p-2" backdropClass="bg-black/30">
              {how().mode !== "watch" ? (
                <MenuItem onSelect={() => { const next = mode() === "touch" ? "trackpad" : "touch"; mode.set(next); session()?.setMode(next); }} class="rounded-lg px-4 py-3 active:bg-accent">
                  <Text class="text-base">{mode() === "touch" ? "Use as trackpad" : "Touch to click"}</Text>
                </MenuItem>
              ) : null}
              <MenuItem onSelect={() => session()?.fit()} class="rounded-lg px-4 py-3 active:bg-accent"><Text class="text-base">Fit screen</Text></MenuItem>
              {status()?.approved && how().mode !== "watch" ? (
                <MenuItem onSelect={() => void act({ keep_awake: !status()!.keep_awake })} class="rounded-lg px-4 py-3 active:bg-accent">
                  <Text class="text-base">{status()!.keep_awake ? "Let this computer sleep" : "Keep this computer awake"}</Text>
                </MenuItem>
              ) : null}
              {how().mode === "watch" ? (
                <MenuItem onSelect={() => { session()?.disconnect(); session.set(null); how.set({ mode: "auto" }); }} class="rounded-lg px-4 py-3 active:bg-accent"><Text class="text-base">Stop watching</Text></MenuItem>
              ) : session()!.phase() === "live" ? (
                <MenuItem onSelect={() => session()?.lose(null)} class="rounded-lg px-4 py-3 active:bg-accent"><Text class="text-base">Disconnect</Text></MenuItem>
              ) : (
                <MenuItem onSelect={() => session()?.connect()} class="rounded-lg px-4 py-3 active:bg-accent"><Text class="text-base">Reconnect</Text></MenuItem>
              )}
            </Menu>
          ) : null}
        </View>
      }
    >
      <View class="flex-1 bg-background">{((key) => untracked(() => (key ? body() : null)))(view().key)}</View>
    </NavPage>
  );
}

/** The live picture, with what lies over it when it is lost, and while watching an agent its bar. */
function Live(props: { session: RemoteSession; status: ScreenStatus; watching: boolean; typing: boolean; onTyping: (on: boolean) => void; onTakeOver: () => void; onStop: () => void }) {
  const agentLabel = () => props.status.agent?.name || "An agent";
  return (
    <View class="flex-1">
      {props.watching ? (
        <View class="flex-row items-center gap-2 border-b border-border px-4 py-2">
          <Text class="flex-1 text-sm">{`Watching ${agentLabel()}`}</Text>
          <Button testID="watch-take-over" onPress={props.onTakeOver} class="rounded-full bg-primary px-3 py-1.5"><Text class="text-sm font-medium text-primary-foreground">Take over</Text></Button>
          <Button accessibilityLabel={`Stop ${agentLabel()}`} onPress={props.onStop} class="rounded-full bg-secondary px-3 py-1.5"><Text class="text-sm font-medium text-destructive">Stop</Text></Button>
        </View>
      ) : null}
      <RemoteView session={props.session} label={`Screen of ${props.status.name ?? "this computer"}`} modifiers={modifiersFor(props.status.platform)} typing={props.typing} onTypingChange={props.onTyping} />
      {props.session.phase() === "connecting" ? (
        <View class="absolute inset-0 items-center justify-center"><Spinner size={28} /></View>
      ) : null}
      {props.session.phase() === "lost" ? (
        <View testID="screen-lost" class="absolute inset-0 items-center justify-center gap-2 bg-background/90 px-8">
          {props.session.locked() ? <Icon name="lock" size={28} /> : null}
          <Text class="text-lg font-semibold">{props.session.locked() ? "Computer is locked" : "Disconnected"}</Text>
          {!props.session.locked() && props.session.reason() ? <Text class="text-center text-sm text-muted-foreground">{props.session.reason()!}</Text> : null}
          <Button testID="screen-reconnect" onPress={() => props.session.connect()} class="mt-2 rounded-full bg-primary px-4 py-2"><Text class="text-sm font-medium text-primary-foreground">Reconnect</Text></Button>
        </View>
      ) : null}
    </View>
  );
}

function AgentCard(props: { status: ScreenStatus; onWatch: (agent: ScreenAgent) => void; onStop: (agent: ScreenAgent) => void; onTakeOver: () => void }) {
  const agents = screenAgents(props.status);
  const label = (agent: ScreenAgent) => agent.name || "An agent";
  return (
    <View class="flex-1 items-center justify-center gap-3 px-6" testID="screen-agents">
      <Icon name="desktop" size={32} />
      <Text class="text-center text-lg font-semibold">{agents.length > 1 ? `${agents.length} agents are using the screen` : `${label(agents[0])} is using the screen`}</Text>
      <View class="w-full max-w-[440px]">
        <Section>
          {agents.map((agent) => (
            <Row
              title={label(agent)}
              trailing={
                <View class="flex-row gap-2">
                  <Button accessibilityLabel={`Watch ${label(agent)}`} onPress={() => props.onWatch(agent)} class="rounded-full bg-secondary px-3 py-1.5"><Text class="text-sm font-medium">Watch</Text></Button>
                  <Button accessibilityLabel={`Stop ${label(agent)}`} onPress={() => props.onStop(agent)} class="rounded-full bg-secondary px-3 py-1.5"><Text class="text-sm font-medium text-destructive">Stop</Text></Button>
                </View>
              }
            />
          ))}
        </Section>
      </View>
      <Button testID="take-over" onPress={props.onTakeOver} class="mt-2 rounded-full bg-primary px-5 py-2.5"><Text class="text-base font-medium text-primary-foreground">{agents.length > 1 ? "Take over (stops all)" : "Take over"}</Text></Button>
    </View>
  );
}

/** This device is the computer: a live view would only mirror itself. */
function LocalCard(props: { status: ScreenStatus; onStop: (agent: ScreenAgent) => void }) {
  const agents = screenAgents(props.status);
  return (
    <View class="flex-1 px-4">
      <Section>
        {agents.length ? agents.map((agent) => <Row icon="desktop" title={agent.name || "An agent"} trailing={<Button accessibilityLabel={`Stop ${agent.name || "An agent"}`} onPress={() => props.onStop(agent)} class="rounded-full bg-secondary px-3 py-1.5"><Text class="text-sm font-medium text-destructive">Stop</Text></Button>} />) : <Row icon="desktop" title="No agent on this screen" />}
      </Section>
    </View>
  );
}

/** The one next step before a viewer can open: unsupported, turned off, not yet allowed, or waiting for permissions. */
function Setup(props: { status: ScreenStatus; name: string; busy: boolean; reason: string | null; onAct: (action: ScreenAction) => void; onCheck: () => void; onChooseComputer: () => void }) {
  const s = props.status;
  if (!s.supported && s.platform === "linux") return <View class="px-4"><VirtualScreenSwitch /></View>;
  if (!s.supported) {
    const unsupported = unsupportedScreen(s, true);
    return <EmptyState icon="desktop" title={unsupported.title} detail={unsupported.detail ?? undefined} action={unsupported.canChooseComputer ? "Choose a computer" : undefined} onAction={props.onChooseComputer} />;
  }
  if (s.disabled) return <EmptyState icon="lock" title="Screen is off" detail={`Run mewla screen enable on ${props.name}.`} action="Check again" onAction={props.onCheck} />;
  if (!s.approved) {
    return (
      <View class="items-center px-6 py-16" testID="screen-enable">
        {props.busy ? <Text class="mb-2 text-center text-lg font-semibold">{`Waiting for Allow on ${props.name}…`}</Text> : props.reason ? <Text class="mb-2 text-center text-sm text-muted-foreground">{props.reason}</Text> : null}
        <Button testID="screen-turn-on" disabled={props.busy} onPress={() => props.onAct({ action: "enable" })} class="flex-row items-center gap-2 rounded-full bg-primary px-5 py-2.5 disabled:opacity-60">
          {props.busy ? <Spinner size={14} tone="paper" /> : null}
          <Text class="text-base font-medium text-primary-foreground">{props.busy ? "Waiting…" : "Turn on Screen"}</Text>
        </Button>
      </View>
    );
  }
  const missing = (s.permissions ?? []).filter((p) => !p.granted);
  const restart = missing.some((p) => p.restart_required) && s.can_restart;
  return (
    <View class="px-4">
      <EmptyState icon="shield-check" title={`Allow Screen on ${props.name}`} action={restart ? undefined : `Ask on ${props.name}`} onAction={() => props.onAct({ action: "enable" })} />
      <Section>
        {missing.map((p) => (
          <Row title={p.name} subtitle={p.guidance} trailing={p.can_open && !p.restart_required ? <Button onPress={() => props.onAct({ action: "open_settings", permission: p.id })} class="rounded-full bg-secondary px-3 py-1.5"><Text class="text-sm font-medium">Open settings</Text></Button> : undefined} />
        ))}
      </Section>
      <View class="mt-4 items-center">
        {restart ? (
          <Button disabled={props.busy} onPress={() => props.onAct({ action: "restart" })} class="rounded-full bg-primary px-5 py-2.5"><Text class="text-base font-medium text-primary-foreground">{props.busy ? "Restarting Mewla…" : "Restart Mewla"}</Text></Button>
        ) : (
          <Button onPress={props.onCheck} class="rounded-full bg-secondary px-5 py-2.5"><Text class="text-base font-medium">Check again</Text></Button>
        )}
        {props.reason ? <Text class="mt-2 text-center text-sm text-destructive line-clamp-2">{props.reason}</Text> : null}
      </View>
    </View>
  );
}

/** Linux: each agent's own display, one card each, listed while they work. */
function AgentDisplays(props: { server: StoredServer; nav: PageNav }) {
  const displays = signal<AgentDisplay[] | null>(null);
  const reason = signal("");
  const error = signal("");
  const sessions = new Map<string, RemoteSession>();
  const read = async () => {
    try {
      const list = await listDisplays(props.server);
      displays.set(list.displays);
      reason.set(list.reason ?? "");
      error.set("");
    } catch (failure) {
      error.set(failureText(failure, "Display request failed"));
    }
  };
  void read();
  effect(() => {
    if (lifecycle() !== "active") return;
    const timer = setInterval(() => void read(), 4_000);
    return () => clearInterval(timer);
  });
  const sessionFor = (worker: string) => {
    let session = sessions.get(worker);
    if (!session) {
      session = new RemoteSession(props.server, { route: displayRoute(worker), align: "top" });
      sessions.set(worker, session);
      session.connect();
    }
    return session;
  };
  const ids = computed(() => (displays() === null ? "loading" : `ids:${displays()!.map((d) => d.id).join(",")}:${reason()}`));
  return (
    <View class="flex-1">
      {error() ? <View class="px-4"><Notice tone="failed" title={error()} /></View> : null}
      {((key) =>
        untracked(() => {
          const list = displays();
          if (list === null) return <EmptyState busy />;
          if (!list.length) return reason() ? <EmptyState icon="construct" title="Browser needs setup on this server" detail={reason()} action="Check again" onAction={() => void read()} /> : <EmptyState icon="desktop" title="Nothing on screen" detail="An agent's screen shows up here while it works." />;
          const shown = list.map((d) => d.id);
          for (const [worker, session] of sessions) if (!shown.includes(worker)) { session.disconnect(); sessions.delete(worker); }
          return <View class="flex-1 gap-3 px-4 pt-2">{list.map((display) => <DisplayCard server={props.server} display={() => displays()?.find((d) => d.id === display.id) ?? display} session={sessionFor(display.id)} onChanged={() => void read()} />)}</View>;
        }))(ids())}
    </View>
  );
}

function DisplayCard(props: { server: StoredServer; display: () => AgentDisplay; session: RemoteSession; onChanged: () => void }) {
  const typing = signal(false);
  const title = () => props.display().id;
  const holding = () => props.session.holding();
  const elsewhere = () => props.display().control === "person" && !holding();
  return (
    <View class="overflow-hidden rounded-2xl border border-border bg-card" testID={`display-${props.display().id}`}>
      <View class="flex-row items-center gap-2 px-4 py-2.5">
        <View class={`size-2 rounded-full ${holding() ? "bg-destructive" : props.display().stopped || elsewhere() ? "bg-muted-foreground" : "bg-primary"}`} />
        <View class="min-w-0 flex-1">
          <Text class="text-base font-medium line-clamp-1">{title()}</Text>
          {holding() ? <Text class="text-xs text-muted-foreground">You hold the agent</Text> : elsewhere() ? <Text class="text-xs text-muted-foreground">Held on another device</Text> : props.display().stopped ? <Text class="text-xs text-muted-foreground">Stopped</Text> : null}
        </View>
        {props.session.phase() === "live" ? <Button accessibilityLabel={typing() ? "Hide keyboard" : "Show keyboard"} onPress={() => typing.set(!typing())} class="p-1.5"><Icon name="keyboard" size={18} /></Button> : null}
        <Menu trigger={<View accessibilityLabel={`Options for ${title()}`} class="p-1.5"><Icon name="more-horizontal" size={18} /></View>} popupClass="rounded-2xl bg-popover p-2" backdropClass="bg-black/30">
          {props.session.phase() === "live" ? (
            <MenuItem onSelect={() => props.session.hold(!holding())} class="rounded-lg px-4 py-3 active:bg-accent"><Text class="text-base">{holding() ? "Let the agent go on" : "Hold the agent"}</Text></MenuItem>
          ) : null}
          {props.display().stopped ? (
            <MenuItem onSelect={() => void resumeDisplay(props.server, props.display().id).then(props.onChanged, () => {})} class="rounded-lg px-4 py-3 active:bg-accent"><Text class="text-base">Resume</Text></MenuItem>
          ) : (
            <MenuItem disabled={elsewhere()} onSelect={() => void haltDisplay(props.server, props.display().id).then(props.onChanged, () => {})} class="rounded-lg px-4 py-3 active:bg-accent disabled:opacity-50"><Text class="text-base text-destructive">{`Stop ${title()}`}</Text></MenuItem>
          )}
          {props.session.phase() !== "live" ? <MenuItem onSelect={() => props.session.connect()} class="rounded-lg px-4 py-3 active:bg-accent"><Text class="text-base">Reconnect</Text></MenuItem> : null}
        </Menu>
      </View>
      {props.session.phase() === "lost" ? <View class="px-4 pb-2"><Notice title="Lost connection to the display" detail={props.session.reason() || "It may have been closed."} action="Reconnect" onAction={() => props.session.connect()} /></View> : null}
      {props.session.phase() === "connecting" ? <View class="flex-row items-center gap-2 px-4 pb-2"><Spinner size={14} /><Text class="text-sm text-muted-foreground">Connecting…</Text></View> : null}
      <View style={{ aspectRatio: (props.display().width || 1280) / (props.display().height || 800) }}>
        <RemoteView session={props.session} label={`Display of ${title()}`} modifiers={modifiersFor("linux")} typing={typing()} onTypingChange={(on) => typing.set(on)} />
      </View>
    </View>
  );
}
