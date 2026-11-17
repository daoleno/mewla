import { cleanup, computed, effect, platform, Scroll, signal, Text, untracked, View, type JSX } from "nsh";
import { Button, Menu, MenuItem } from "nsh/base";
import { displayIssue, type DisplayIssue } from "../../../core/screen/displayIssue";
import { displayRoute } from "../../../core/screen/displayRoute";
import { haltDisplay, listDisplays, resumeDisplay, type AgentDisplay } from "../../../core/screen/screen";
import { hostLabel, pickStageAgent, stageAgents, type StageAgent } from "../../../core/screen/stage";
import type { StoredServer } from "../../../core/storedServerContract";
import { workers } from "../../session";
import { agentDisplays } from "../../screenfloat/agentDisplays";
import { EmptyState, Notice, Spinner } from "../../kit/kit";
import { Icon } from "../../kit/Icon";
import { inFront } from "../../kit/inFront";
import { GuardedSheet as Sheet } from "../../shell/AppSheet";
import { wide } from "../../shell/layout";
import { canShowFullSize, modifiersFor, needsSoftKeyboard, RemoteSession, RemoteView } from "./RemoteView";

/** How often the stage reads the computer's displays again, so an agent that starts or ends shows up. */
const REFRESH_MS = 4_000;
const ITEM = "min-h-[48px] justify-center rounded-xl px-4 py-2.5 hover:bg-accent active:bg-accent focus:bg-accent disabled:opacity-40";

/**
 * Screen's stage for agents' displays (a Linux computer's, a Mac's agent windows): one agent at a time, full
 * bleed, one live view. The chip (tabs on a wide window) switches agents; Take over holds the agent and, for
 * a browser window held from a phone, lays its page out for the phone; Give back lets it go on. Leaving the
 * page, a switch of computer or the app going to the background closes the view, which gives it back.
 */
export interface Stage {
  /** The computer's displays, null until the first read. */
  displays: () => AgentDisplay[] | null;
  agents: () => StageAgent[];
  current: () => StageAgent | null;
  session: () => RemoteSession | null;
  reason: () => string;
  issue: () => DisplayIssue | null;
  typing: () => boolean;
  setTyping: (on: boolean) => void;
  switcher: () => boolean;
  setSwitcher: (on: boolean) => void;
  busy: () => boolean;
  checking: () => boolean;
  /** This device holds the agent shown. */
  holding: () => boolean;
  /** Someone else holds it. */
  heldElsewhere: () => boolean;
  /** The page's host while its window is laid out for this view. */
  host: () => string;
  live: () => boolean;
  pick: (id: string) => void;
  takeOver: () => void;
  giveBack: () => void;
  stop: () => void;
  resume: () => void;
  check: () => void;
}

/**
 * The stage's state, for the page that shows it: `active` while the page shows agents' displays. `focus` is the
 * Worker asked for (a Work card, the float's Open); `takeOver` takes it over as soon as its view is live (a Work
 * card's Take over).
 */
export function createStage(opts: { server: () => StoredServer | null; active: () => boolean; focus?: string; takeOver?: boolean }): Stage {
  const displays = signal<AgentDisplay[] | null>(null);
  const reason = signal("");
  const issue = signal<DisplayIssue | null>(null);
  const checking = signal(false);
  const picked = signal<string | null>(null);
  const session = signal<RemoteSession | null>(null);
  const typing = signal(false);
  const switcher = signal(false);
  const busy = signal(false);
  let shown: string | null = null;
  let pendingTakeOver = opts.takeOver ? opts.focus : undefined;
  let serverId = "";
  let gone = false;

  const names = (id: string) => workers().find((w) => w.id === id)?.name;
  const agents = computed(() => stageAgents(displays() ?? [], names), {
    equals: (a, b) => a.length === b.length && a.every((row, i) => JSON.stringify(row) === JSON.stringify(b[i])),
  });
  const currentId = computed(() => {
    const id = pickStageAgent(agents(), picked() ?? opts.focus, shown);
    shown = id;
    return id;
  });
  const current = computed(() => agents().find((agent) => agent.id === currentId()) ?? null, { equals: (a, b) => JSON.stringify(a) === JSON.stringify(b) });

  const fail = (failure: unknown) => {
    const server = untracked(opts.server);
    if (!gone && server) issue.set(displayIssue(failure, server.name || "this computer"));
  };
  const read = async () => {
    const server = untracked(opts.server);
    if (!server) return;
    try {
      const list = await listDisplays(server);
      if (gone || untracked(opts.server)?.id !== server.id) return;
      displays.set(list.displays);
      // The same list the floating screen and the Work cards read: one model of the computer's displays.
      agentDisplays.set(list.displays);
      reason.set(list.reason ?? "");
      issue.set(null);
    } catch (failure) {
      if (gone || untracked(opts.server)?.id !== server.id) return;
      // A first read that fails still ends the wait: what is known is nothing.
      if (untracked(displays) === null) displays.set([]);
      fail(failure);
    }
  };

  // A new computer: nothing of the last one's stays.
  effect(() => {
    const id = opts.server()?.id ?? "";
    untracked(() => {
      if (id === serverId) return;
      serverId = id;
      displays.set(null);
      picked.set(null);
      shown = null;
      issue.set(null);
    });
  });
  // Reads while the stage shows and the app is in front.
  effect(() => {
    if (!opts.active() || !inFront() || !opts.server()) return;
    void read();
    const timer = setInterval(() => void read(), REFRESH_MS);
    return () => clearInterval(timer);
  });

  // One live view: the agent shown, while the stage shows and the app is in front. Another agent, another
  // computer or the app going to the background closes it (a hold ends with its socket).
  let bound = "";
  effect(() => {
    const id = currentId();
    const server = opts.server();
    const on = opts.active() && inFront();
    untracked(() => {
      const key = on && id && server ? `${server.id}|${id}` : "";
      if (key === bound) return;
      bound = key;
      session()?.disconnect();
      typing.set(false);
      if (!key || !server || !id) {
        session.set(null);
        return;
      }
      // From a phone (not a desktop or a wide window), holding lays the agent's page out for the phone.
      const next = new RemoteSession(server, { route: displayRoute(id), align: "center", reflow: !wide() && !platform.desktop });
      session.set(next);
      next.connect();
    });
  });
  // A Work card's Take over: hold the agent as soon as its view is live.
  effect(() => {
    const s = session();
    const live = s?.phase() === "live";
    untracked(() => {
      if (!s || !live || !pendingTakeOver || currentId() !== pendingTakeOver) return;
      pendingTakeOver = undefined;
      s.hold(true);
    });
  });
  cleanup(() => {
    gone = true;
    untracked(session)?.disconnect();
  });

  const holding = () => !!session()?.holding();
  const run = async (action: () => Promise<unknown>) => {
    if (untracked(busy)) return;
    busy.set(true);
    try {
      await action();
      await read();
    } catch (failure) {
      fail(failure);
    } finally {
      busy.set(false);
    }
  };
  return {
    displays,
    agents,
    current,
    session,
    reason,
    issue,
    typing,
    setTyping: (on) => typing.set(on),
    switcher,
    setSwitcher: (on) => switcher.set(on),
    busy,
    checking,
    holding,
    heldElsewhere: () => !!current()?.held && !holding(),
    host: () => (session()?.reflow().on ? hostLabel(session()!.reflow().host) : ""),
    live: () => session()?.phase() === "live",
    pick: (id) => {
      picked.set(id);
      switcher.set(false);
    },
    takeOver: () => untracked(session)?.hold(true),
    giveBack: () => untracked(session)?.hold(false),
    stop: () => {
      const id = untracked(currentId);
      const server = untracked(opts.server);
      if (id && server) void run(() => haltDisplay(server, id));
    },
    resume: () => {
      const id = untracked(currentId);
      const server = untracked(opts.server);
      if (id && server) void run(() => resumeDisplay(server, id));
    },
    check: () => {
      if (untracked(checking)) return;
      checking.set(true);
      void read().finally(() => checking.set(false));
    },
  };
}

/** A round letter for an agent, as the switcher and the chip show it. */
function Avatar(props: { name: string; held?: boolean }) {
  return (
    <View class={`size-6 items-center justify-center rounded-full ${props.held ? "bg-white" : "bg-white/85"}`}>
      <Text class={`text-caption font-bold ${props.held ? "text-seal" : "text-black"}`}>{Array.from(props.name.trim())[0]?.toUpperCase() ?? "A"}</Text>
    </View>
  );
}

/** The agent shown, in the bar: a chip that opens the switcher (a wide window has tabs instead, `StageTabs`). Red while you hold it. */
export function StageChip(props: { stage: Stage }) {
  const name = () => props.stage.current()?.name ?? "Screen";
  const held = () => props.stage.holding();
  const several = () => props.stage.agents().length > 1;
  return (
    <Button testID="stage-chip" accessibilityLabel={several() ? `${name()}, switch agent` : name()} onPress={() => props.stage.setSwitcher(true)}
      class={`h-9 max-w-full min-w-0 flex-row items-center gap-2 rounded-full pl-1.5 pr-3 active:opacity-70 ${held() ? "bg-seal" : "bg-white/15"}`}>
      <Avatar name={name()} held={held()} />
      <Text class="min-w-0 shrink text-body font-semibold text-white line-clamp-1">{name()}</Text>
      <If when={!held() && props.stage.current()?.acting}><View class="size-1.5 rounded-full bg-white" /></If>
      <Icon name="chevron-down" size={14} tone="onBlack" />
    </Button>
  );
}

/** Every agent as a tab, on a wide window: one tap switches. */
export function StageTabs(props: { stage: Stage }) {
  return (
    <Scroll horizontal keyboardShouldPersistTaps="handled" class="min-w-0 flex-1 grow-0" contentClass="flex-row items-center gap-2">
      {keyedRows(props.stage, (agent) => {
        const selected = () => props.stage.current()?.id === agent.id;
        const held = () => selected() && props.stage.holding();
        return (
          <Button testID={`stage-tab-${agent.id}`} accessibilityLabel={agent.name} selected={selected()} onPress={() => props.stage.pick(agent.id)}
            class={`h-9 flex-row items-center gap-2 rounded-full pl-1.5 pr-3 hover:opacity-80 ${held() ? "bg-seal" : selected() ? "bg-white/20" : "bg-transparent"}`}>
            <Avatar name={agent.name} held={held()} />
            <Text class={`text-body font-semibold line-clamp-1 ${selected() ? "text-white" : "text-white/60"}`}>{agent.name}</Text>
            <If when={agent.acting && !held()}><View class="size-1.5 rounded-full bg-white" /></If>
          </Button>
        );
      })}
    </Scroll>
  );
}

/** The rows, made again only when which agents there are changes. */
function keyedRows(stage: Stage, row: (agent: StageAgent) => JSX.Element): JSX.Element {
  const ids = computed(() => stage.agents().map((a) => `${a.id}\n${a.name}\n${a.acting}`).join("\n\n"));
  return ((_key: string) => untracked(() => stage.agents().map(row)))(ids());
}

/** The page's host while the agent's window is laid out for this view: the real one, whatever the page says. */
export function DomainChip(props: { host: string }) {
  return (
    <View testID="stage-domain" accessibilityLabel={`Page on ${props.host}`} class="h-8 min-w-0 max-w-[132px] shrink flex-row items-center gap-1.5 rounded-full bg-white/15 px-3">
      <Icon name="lock" size={12} tone="onBlack" />
      <Text class="min-w-0 shrink text-sub font-semibold text-white line-clamp-1">{props.host}</Text>
    </View>
  );
}

/** The one hard switch: Take over, or (red) Give back. */
export function TakeOverButton(props: { holding: boolean; disabled?: boolean; compact?: boolean; testID?: string; onTakeOver: () => void; onGiveBack: () => void }) {
  return (
    <Button testID={props.testID ?? "stage-take-over"} accessibilityLabel={props.holding ? "Give back" : "Take over"} disabled={props.disabled} onPress={() => (props.holding ? props.onGiveBack() : props.onTakeOver())}
      class={`flex-row items-center gap-2 rounded-full active:opacity-80 disabled:opacity-40 ${props.compact ? "h-9 px-3.5" : "h-12 px-6"} ${props.holding ? "bg-seal" : "bg-white"}`}>
      <Icon name={props.holding ? "arrow-forward" : "hand"} size={props.compact ? 14 : 18} tone={props.holding ? "onBlack" : "ink"} />
      <Text class={`font-semibold ${props.compact ? "text-sub" : "text-body"} ${props.holding ? "text-white" : "text-black"}`}>{props.holding ? "Give back" : "Take over"}</Text>
    </Button>
  );
}

/** The stage bar's end: the page's host, the keyboard, on a wide window the switch, and ⋯. */
export function StageBarEnd(props: { stage: Stage; platform?: string; menuItems?: JSX.Element; hasMenuItems?: () => boolean; ownScreen?: { title: string; open: () => void } }) {
  const stage = props.stage;
  const roomy = () => wide() || platform.desktop;
  return (
    <View class="min-w-0 flex-row items-center gap-1">
      <If when={stage.host()}>{(host) => <DomainChip host={host()} />}</If>
      <If when={roomy() && stage.current()}><View class="ml-1"><TakeOverButton compact testID="stage-take-over-bar" holding={stage.holding()} disabled={!stage.live() || stage.heldElsewhere() || stage.busy()} onTakeOver={stage.takeOver} onGiveBack={stage.giveBack} /></View></If>
      <If when={stage.live() && needsSoftKeyboard()}><Button testID="stage-keyboard" accessibilityLabel={stage.typing() ? "Hide keyboard" : "Show keyboard"} selected={stage.typing()} onPress={() => stage.setTyping(!stage.typing())}
          class={`size-11 items-center justify-center rounded-full active:bg-white/15 ${stage.typing() ? "bg-white/20" : ""}`}>
          <Icon name="keyboard" size={20} tone={stage.typing() ? "onBlack" : "onBlackSoft"} />
        </Button></If>
      {/* ⋯ only while it has something in it: an agent shown, the computer's own screen, the Phone row. */}
      <View style={{ display: stage.current() || props.ownScreen || props.hasMenuItems?.() ? "flex" : "none", flexDirection: "row" }}>
      <Menu trigger={<View testID="screen-options" accessibilityLabel="Screen options" role="button" class="size-11 items-center justify-center rounded-full active:bg-white/15"><Icon name="more-horizontal" size={20} tone="onBlack" /></View>}
        popupClass={`rounded-[18px] bg-popover p-1.5 shadow-float ${wide() ? "w-[288px]" : ""}`} backdropClass="bg-scrim">
        <If when={stage.current()}>{(agent) => <View class="px-4 pb-1 pt-2"><Text class="text-caption font-semibold text-muted-foreground line-clamp-1">{agent().name}</Text></View>}</If>
        <If when={stage.live()}><MenuItem testID="stage-fit" onSelect={() => stage.session()?.fitView()} class={ITEM}><Text class="text-body">Fit</Text></MenuItem></If>
        <If when={stage.live() && canShowFullSize()}><MenuItem testID="display-full-size" onSelect={() => void stage.session()?.fullSize(`Display of ${untracked(stage.current)?.name ?? ""}`)} class={ITEM}><Text class="text-body">Full size</Text></MenuItem></If>
        <If when={stage.current()}><If when={stage.current()?.stopped} else={<MenuItem testID="stage-stop-menu" disabled={stage.busy() || stage.heldElsewhere()} onSelect={stage.stop} class={ITEM}><Text class="text-body text-seal">Stop</Text></MenuItem>}><MenuItem disabled={stage.busy()} onSelect={stage.resume} class={ITEM}><Text class="text-body">Resume</Text></MenuItem></If></If>
        <If when={stage.session()?.phase() === "lost"}><MenuItem onSelect={() => stage.session()?.connect()} class={ITEM}><Text class="text-body">Reconnect</Text></MenuItem></If>
        <If when={props.ownScreen}>{(own) => <MenuItem testID="displays-own-screen" onSelect={() => own().open()} class={ITEM}><Text class="text-body">{`Screen of ${own().title}`}</Text></MenuItem>}</If>
        {props.menuItems ?? null}
      </Menu>
      </View>
    </View>
  );
}

/**
 * The stage itself: the agent's live view, full bleed, a red frame while you hold it, and on a phone the switch
 * and Stop at the bottom (a wide window has them in the bar). Typing docks the key bar above the keyboard instead.
 */
export function StageBody(props: { stage: Stage; platform?: string; serverName: string; ownScreen?: { title: string; open: () => void } }) {
  const stage = props.stage;
  const notice = () => <If when={stage.issue()}>{(current) => <Notice testID="displays-issue" tone={current().tone === "danger" ? "failed" : "warning"} title={current().title} detail={current().detail} />}</If>;
  const kind = computed(() => (stage.displays() === null ? "loading" : stage.displays()!.length ? "stage" : stage.reason() ? "setup" : "empty"));
  return (
    <View class="flex-1">
      {((key) =>
        untracked(() => {
          if (key === "loading") return <View class="flex-1 px-4">{notice()}<EmptyState onBlack busy /></View>;
          if (key === "empty") return <View class="flex-1 px-4">{notice()}<EmptyState onBlack icon="desktop" title="Nothing on screen" /></View>;
          if (key === "setup") {
            return (
              <View class="flex-1 px-4">
                {notice()}
                <View class="items-center gap-2 px-6 py-16">
                  <Icon name="construct" size={28} tone="onBlack" />
                  <Text class="mt-1 text-center text-title font-semibold text-white">Browser needs setup on this server</Text>
                  <Text class="text-center text-sub text-white/60">{stage.reason()}</Text>
                  <Button testID="displays-check" disabled={stage.checking()} onPress={stage.check} class="mt-2 h-[52px] flex-row items-center gap-2 rounded-2xl bg-white px-5 active:opacity-70 disabled:opacity-40">
                    <If when={stage.checking()}><Spinner size={14} tone="onBlackSoft" /></If>
                    <Text class="text-body font-semibold text-black">Check again</Text>
                  </Button>
                </View>
              </View>
            );
          }
          return <StagePicture stage={stage} platform={props.platform} notice={notice} />;
        }))(kind())}
      <StageSwitcher stage={stage} serverName={props.serverName} ownScreen={props.ownScreen} />
    </View>
  );
}

function StagePicture(props: { stage: Stage; platform?: string; notice: () => JSX.Element }) {
  const stage = props.stage;
  const roomy = () => wide() || platform.desktop;
  return (
    <View class="flex-1">
      {props.notice()}
      <View class="flex-1">
        <If when={!stage.session()}><View class="absolute inset-0 items-center justify-center"><Spinner size={28} tone="onBlackSoft" /></View></If>
        {/* One view per session: another agent is another view. */}
        <Each of={stage.session() ? [stage.session()!] : []} key={(s) => s}>{(item) => {
              const s = untracked(item);
              return (
              <View testID={`display-${untracked(stage.current)?.id ?? ""}`} class="flex-1">
                <RemoteView session={s} label={`Display of ${untracked(stage.current)?.name ?? ""}`} modifiers={modifiersFor(props.platform ?? "linux")} typing={stage.typing()} onTypingChange={stage.setTyping} />
                <If when={s.phase() === "connecting"}><View class="absolute inset-0 items-center justify-center" style={{ pointerEvents: "none" }}><Spinner size={28} tone="onBlackSoft" /></View></If>
                <If when={s.phase() === "lost"}><View testID="stage-lost" class="absolute inset-0 items-center justify-center gap-3 bg-black px-8">
                    <Icon name="cloud-offline" size={36} tone="onBlack" />
                    <Text class="text-center text-title font-semibold text-white">Disconnected</Text>
                    <If when={s.reason()}><Text class="text-center text-sub text-white/60 line-clamp-2">{s.reason()!}</Text></If>
                    <Button testID="stage-reconnect" onPress={() => s.connect()} class="h-11 items-center justify-center rounded-full bg-white px-5 active:opacity-70"><Text class="text-body font-semibold text-black">Reconnect</Text></Button>
                  </View></If>
              </View>
              );
            }}</Each>
        {/* While you hold the agent: the seal's red around the stage. */}
        <If when={stage.holding()}><View testID="stage-held-frame" class="absolute inset-0 border-[3px] border-seal" style={{ pointerEvents: "none" }} /></If>
      </View>
      <If when={!roomy() && !stage.typing() && stage.current()}><View testID="stage-bar" class="flex-row items-center justify-center gap-3 bg-black px-4 py-3">
          <TakeOverButton holding={stage.holding()} disabled={!stage.live() || stage.heldElsewhere() || stage.busy()} onTakeOver={stage.takeOver} onGiveBack={stage.giveBack} />
          <If when={stage.current()?.stopped} else={<Button testID="stage-stop" accessibilityLabel={`Stop ${stage.current()?.name ?? "the agent"}`} disabled={stage.busy() || stage.heldElsewhere()} onPress={stage.stop}
              class="size-12 items-center justify-center rounded-full bg-white/15 active:bg-white/25 disabled:opacity-40">
              <Icon name="stop" size={18} tone="seal" />
            </Button>}><Button testID="stage-resume" accessibilityLabel={`Resume ${stage.current()?.name ?? "the agent"}`} disabled={stage.busy()} onPress={stage.resume}
              class="size-12 items-center justify-center rounded-full bg-white/15 active:bg-white/25 disabled:opacity-40">
              <Icon name="play" size={18} tone="onBlack" />
            </Button></If>
        </View></If>
    </View>
  );
}

/** Which agent the stage shows: every agent on this computer, and the computer's own screen where it has one. */
function StageSwitcher(props: { stage: Stage; serverName: string; ownScreen?: { title: string; open: () => void } }) {
  const stage = props.stage;
  const state = (agent: StageAgent) =>
    agent.stopped ? "Stopped" : agent.id === stage.current()?.id && stage.holding() ? "You have it" : agent.held ? "Held on another device" : agent.acting ? "Working" : "";
  return (
    <Sheet testID="stage-switcher" open={stage.switcher()} onOpenChange={({ open }) => !open && stage.setSwitcher(false)} closeOnSwipe popupClass="max-h-[80%] rounded-t-(--radius-sheet) bg-sheet pb-8 pt-3 shadow-float" sidePadding={16} backdropClass="bg-scrim">
      <View class="gap-1 px-1">
        <Text class="px-3 pb-1 text-caption font-semibold text-muted-foreground line-clamp-1">{props.serverName}</Text>
        {keyedRows(stage, (agent) => (
          <Button testID={`stage-agent-${agent.id}`} accessibilityLabel={agent.name} selected={stage.current()?.id === agent.id} onPress={() => stage.pick(agent.id)}
            class="min-h-[56px] flex-row items-center gap-3 rounded-2xl px-3 active:bg-accent">
            <View class="size-8 items-center justify-center rounded-full bg-track"><Text class="text-sub font-bold">{Array.from(agent.name.trim())[0]?.toUpperCase() ?? "A"}</Text></View>
            <View class="min-w-0 flex-1">
              <Text class="text-body font-semibold line-clamp-1">{agent.name}</Text>
              <If when={state(agent)}><Text class={`text-caption line-clamp-1 ${state(agent) === "You have it" ? "text-seal" : "text-muted-foreground"}`}>{state(agent)}</Text></If>
            </View>
            <If when={stage.current()?.id === agent.id}><Icon name="check" size={18} tone="ink" /></If>
          </Button>
        ))}
        <If when={props.ownScreen}>{(own) => (
          <Button testID="stage-own-screen" accessibilityLabel={`Screen of ${own().title}`} onPress={() => { stage.setSwitcher(false); own().open(); }}
            class="min-h-[56px] flex-row items-center gap-3 rounded-2xl px-3 active:bg-accent">
            <View class="size-8 items-center justify-center rounded-full bg-track"><Icon name="desktop" size={16} tone="ink" /></View>
            <Text class="min-w-0 flex-1 text-body font-semibold line-clamp-1">{own().title}</Text>
          </Button>
        )}</If>
      </View>
    </Sheet>
  );
}
