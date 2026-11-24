import { cleanup, computed, effect, openUrl, platform, safeArea, untracked, signal, Text, View, type JSX } from "nsh";
import { CAPSULE, capsuleInset, capsuleRailInset } from "../../core/shell/capsule";
import { brainThreadReadingScope } from "../../core/timeline/readingPlace";
import { Capsule, CapsulePage, createCapsule, type CapsuleReading } from "../shell/Capsule";
import { Button } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { NavPage } from "../shell/NavPage";
import { Composer as ChatComposer, createComposerText } from "../chat/Composer";
import { createMessageEdit, EditChip, editOnTap } from "../chat/messageEdit";
import { bringRequestBack, durableAnswered, RequestStack, requestsInView, threadNewest, threadRequests, threadSealed } from "../chat/RequestCard";
import { ChatTimeline } from "../chat/Timeline";
import { derived, preparedRows } from "../kit/derived";
import { ThreadWait } from "../chat/ThreadWait";
import { brainHostIdentity } from "../chat/links";
import { chatImage } from "../chat/assets";
import { stopTurn } from "../../core/conversation/turn";
import { brainAgentInstalls, brainSurface, isBrainThreadCacheKey, newBrainChat } from "../../core/brain";
import { createChatTimeline } from "../../core/conversation/timeline";
import type { BrainWorkResultEvent } from "../../core/timeline/brainWorkEvent";
import type { BrainCurrentWork } from "../../core/timeline/types";
import { workForCards } from "../../core/timeline/model";
import { markWorkRead } from "../../core/work/brainWork";
import { goHome, homeShown, openBrainTerminal, openPage, openSession, openWorkDetail, trayOpen } from "../shell/nav";
import { lastEditablePrompt } from "../../core/web/composer";
import { workspaceRoute } from "../more/files/routes";
import { brain, brainChat, brainDraft, brainThread, connection, current, currentTransport, openBrainThread, routedBrainThread, servers, workers } from "../session";
import { ResultActions, type WorkCardHandlers } from "../chat/TimelineRows";
import type { BrainOpenWork, ResultAction } from "../../core/work/types";
import type { WorkCardModel } from "../../core/timeline/workCardModel";
import { sessionRowModel } from "../../core/sessions/listRow";
import { workWorkerLines } from "../../core/work/workers";
import { presentWorker } from "../../core/sessions/workerPresentation";
import { Sheet } from "../kit/Sheet";
import { PillButton } from "../kit/kit";
import { clock } from "../calendar/clock";
import { Icon } from "../kit/Icon";
import { linkHooks } from "../kit/wait";
import { HomeCat } from "./Cat";
import { isMacApp } from "../mac/app";
import { MacNoAgent } from "../mac/NoAgent";
import { MacReady } from "../mac/Ready";
import { MacWaking } from "../mac/Starting";
import { macLightsRow } from "../mac/local";
import { StateControl } from "../tray/StateControl";
import { ChatUpdateRow, chatUpdate } from "../update/ChatUpdateRow";
import { ProviderOutageRow, providerOutage } from "./ProviderOutageRow";
import { HOME_HEADER_HEIGHT, homeHeaderInset, Tray } from "../tray/Tray";
import { BrainChatFailed, BrainChatUnavailable, BrainHostSheet, BrainMenu, brainChatState, brainChatUnavailable, brainComposerBlock, createBrainComposer, currentBrain, MentionPanel } from "../brain";
import { newSessionOpen } from "../screens/NewSession";
import { ComposerPlusButton, SlashMenu } from "../session/SlashMenu";
import { CodexSkillsSheet } from "../session/CodexSkillsSheet";
import { CodexStatusSheet } from "../session/CodexStatusSheet";
import { AttachmentTray } from "../more/files/Pages";
import { tray } from "../tray/state";
import { rail, wide } from "../shell/layout";
import { createMenuAnchor } from "../kit/Menu";
import type { UpdateLineItem } from "../../core/timeline/updateLines";
import { brainComposing, brainNeedsPerson, lines as updateLines } from "../whatsnew/state";
import { density, headerSize } from "../kit/density";
import { FileDrop } from "../chat/FileDrop";
import { attachIncoming, type IncomingFiles } from "../more/files/incoming";

const startingChat = signal(false);
const notice = signal("");
/** Brain's thread's open requests (SPEC §1.1): Brain's own and those of the Sessions working for its Work. */
const requests = threadRequests({ brain: true });
/** Those the foot shows: the ones put aside ("Later") stay in the tray only. */
const requestsHere = requestsInView(requests);
/** The request the person (or a tray row) opened in the stack; none opens the oldest. */
const pickedRequest = signal("");
/** Bumped to bring the open request into view. */
const revealRequests = signal(0);

/** A tray row of a request in Brain's thread (the tray has closed): the thread scrolls to that card, open. */
export function showRequestCard(key: string): void {
  bringRequestBack(key);
  pickedRequest.set(key);
  if (!homeShown()) goHome();
  revealRequests.set(revealRequests() + 1);
}
const activity = computed(() => brainThread().conversation?.activity);
/** The person's sends from Brain's composer: each brings the sent message to the top of the thread (as a Session's do). */
const brainSends = signal(0);
/** The activity a Stop was sent for: the button says "Stopping…" until that turn ends. */
const stoppingFor = signal("");
const timeline = createChatTimeline("brain");
function readWork(workId: string): void {
  const transport = connection() === "connected" ? currentTransport() : null;
  if (transport) markWorkRead(transport, workId);
}
// A Work card's header (or its Session's line) opens the Session that ran it; without one listed, its Work detail.
const openWorkEvent = (event: BrainWorkResultEvent, canOpenSession: boolean) => {
  if (event.unread) readWork(event.work_id);
  if (canOpenSession && event.session_id) openSession(event.session_id);
  else openWorkDetail(event.work_id);
};
/** The result sheet a Work's quiet line opens (SPEC §1.2): who, when, the summary, Next step, Open session. */
const resultSheet = signal<WorkCardModel | null>(null);
/** A result's next step: where it names; using it reads the result, so the card becomes its line. */
function runResultAction(model: WorkCardModel, action: ResultAction): void {
  if (model.unread) readWork(model.workId);
  resultSheet.set(null);
  switch (action.kind) {
    case "pull_request":
      return void openUrl(action.url).catch(() => notice.set("Couldn't open the pull request."));
    case "git_review":
      return openPage({ name: "git", targetId: action.session_id, cwd: action.cwd });
    case "file":
      return openPage({ name: "git-file", targetId: action.session_id, cwd: action.cwd, path: action.path });
    case "brain_file":
      return openPage({ name: "files-workspace", file: action.path, hostSession: brainHostIdentity() ?? undefined });
    case "brain_folder":
      return openPage({ name: "files-workspace", trail: [action.path], hostSession: brainHostIdentity() ?? undefined });
  }
}
const workHandlers: WorkCardHandlers = {
  agentOf: (sessionId) => {
    const worker = sessionId ? workers().find((w) => w.id === sessionId) : undefined;
    if (!worker) return null;
    const row = sessionRowModel(worker);
    return { kind: row.kind, flavor: row.flavor, name: presentWorker(worker).typeLabel };
  },
  onTitle: (workId) => openWorkDetail(workId),
  workers: (rows) => workWorkerLines(rows, new Map(workers().map((w) => [w.id, w] as const))),
  onSession: (sessionId) => openSession(sessionId),
  onLine: (model) => resultSheet.set(model),
  onAction: runResultAction,
};
// A past thread is read-only: its Work cards and messages offer no actions. Derived, not computed: an update whose
// projection a slow phone cut off (NSH's 2 s budget) must not stop the thread for the rest of the app's life.
// The rows keep the chat they were made from: the thread shown changes before its chat does (and the rows lag
// while a long thread is prepared), and a thread's list takes only its own thread's rows.
const prepared = preparedRows(() => brainThread().conversation?.events ?? [], () => {
  const state = brainThread();
  return {
    cacheKey: state.cacheKey,
    rows: timeline(state, {
      onPendingAction: routedBrainThread() ? undefined : (id, action) => untracked(brainChat)?.actOnFailed(id, action),
      onWorkEvent: routedBrainThread() ? undefined : openWorkEvent,
      openSessionIds: new Set(workers().map((w) => w.id)),
      currentWork: workForCards(brain()?.current_work as readonly BrainCurrentWork[] | undefined, brain()?.open_work as readonly BrainOpenWork[] | undefined),
    }),
  };
}, { cacheKey: "", rows: [] });
const items = () => prepared().rows;
type ThreadRows = ReturnType<typeof items>;
const NO_ROWS: ThreadRows = [];
/** The rows of one thread: none while the rows shown are another thread's (a list opens on its first rows). */
const threadRows = (threadId: string): ThreadRows => {
  const made = prepared();
  return isBrainThreadCacheKey(made.cacheKey, threadId) ? made.rows : NO_ROWS;
};
const durableHere = durableAnswered(items);
const newestHere = threadNewest(items);
// What's New waits while a card here needs the person.
effect(() => brainNeedsPerson.set(requestsHere().length > 0));
/** The lines updates left (What's New), in Brain's current chat only; one object per line while it is the same. */
const lineItems = new Map<string, UpdateLineItem>();
const updateLineRows = derived((): readonly UpdateLineItem[] => {
  if (routedBrainThread()) return [];
  return updateLines().map((line) => {
    const id = `update:${line.version}`;
    const kept = lineItems.get(id);
    if (kept && kept.highlights === line.highlights && kept.notes === line.notes) return kept;
    const item: UpdateLineItem = { type: "update-line", id, timestamp: new Date(line.at).toISOString(), version: line.version, highlights: line.highlights, notes: line.notes };
    lineItems.set(id, item);
    return item;
  });
});
const sealedHere = threadSealed({ brain: true }, durableHere, newestHere);

function hostId(): string {
  return brain()?.host_worker?.id ?? "";
}

async function stop(): Promise<void> {
  const transport = currentTransport();
  const running = activity();
  if (!transport || !hostId() || !running || running.status !== "running") return;
  stoppingFor.set(running.id);
  try {
    await stopTurn(transport, hostId());
  } catch {
    stoppingFor.set("");
    notice.set("Could not stop Brain. Try again.");
  }
}

function surface() {
  return brainSurface({ hasServer: Boolean(current()), connected: connection() === "connected", snapshot: brain() });
}

/** New chat (Brain's menu, the desktop palette). Rejects when the computer did not start one: the menu says so. */
export async function startNewChat(): Promise<void> {
  const transport = currentTransport();
  if (!transport) throw new Error("offline");
  if (startingChat()) return;
  startingChat.set(true);
  try {
    await newBrainChat(transport);
    // A past thread was showing: the new chat is what the person asked for.
    openBrainThread(null);
  } finally {
    startingChat.set(false);
  }
}

/** What the thread area holds while it has no rows (SPEC §1.3): loading, couldn't load, or nothing yet. */
const chatState = computed(() => brainChatState({
  itemCount: items().length,
  loading: brainThread().loading,
  error: brainThread().error,
  conversation: brainThread().conversation as { available?: boolean; reason?: string } | null | undefined,
}));

/**
 * Brain's word in its empty thread. On a phone it is centred; on wide the pane is tall and the words would float
 * far above the composer they invite to (the owner's wide list, 11-13), so they sit on the bottom, just above it.
 */
function Notice(props: { title: string; detail?: string }) {
  return (
    <View class={`flex-1 items-center gap-2 p-8 ${wide() ? "justify-end pb-6" : "justify-center"}`}>
      <Text testID="brain-notice" class="text-center text-title font-semibold tracking-title">{props.title}</Text>
      <If when={props.detail}>{(detail) => <Text class="text-center text-sub text-muted-foreground">{detail()}</Text>}</If>
    </View>
  );
}

/** A past thread a notification opened: read-only, with the way back to Brain's current chat (SPEC §1.6). */
function PastThread() {
  return (
    <View testID="brain-past-thread" class="mx-4 mb-2 flex-row items-center gap-3 rounded-(--radius-group) bg-card px-4 py-3">
      <Text class="flex-1 text-sub text-muted-foreground">Earlier thread · read-only</Text>
      <Button testID="brain-current-thread" onPress={() => openBrainThread(null)} class="min-h-[44px] justify-center active:opacity-70">
        <Text class="text-sub font-semibold">Current chat</Text>
      </Button>
    </View>
  );
}

/** A Work's result, from its quiet line: who, when, what came back, the next step and its Session. */
function ResultSheet() {
  const model = () => resultSheet();
  const agent = () => workHandlers.agentOf(model()?.sessionId);
  const when = () => (model()?.at ? clock().format.dateTime(model()!.at!, clock().zones.device) : "");
  const close = () => resultSheet.set(null);
  return (
    <Sheet testID="work-result-sheet" open={() => Boolean(resultSheet())} onClose={close} title={() => model()?.title ?? ""}>
      <If when={model()}><View class="gap-3 px-4">
          <Text class="text-sub text-muted-foreground">{[agent()?.name, model()!.state, when()].filter(Boolean).join(" · ")}</Text>
          <If when={model()!.summary}><View selectable><Text class="text-body">{model()!.summary}</Text></View></If>
          <If when={model()!.actions.length}><ResultActions actions={model()!.actions} onPress={(action) => runResultAction(untracked(resultSheet)!, action)} /></If>
          <If when={model()!.sessionId && workers().some((w) => w.id === model()!.sessionId)}><PillButton testID="work-sheet-open-session" title="Open session" onPress={() => {
              const id = untracked(resultSheet)!.sessionId!;
              close();
              openSession(id);
            }} /></If>
        </View></If>
    </Sheet>
  );
}

/** Brain's menu is open (the header's ⋯). */
const menuOpen = signal(false);

/** Home with the Brain host sheet open (the computer page's "Claude is not signed in" › Switch). */
export function openBrainHost(): void {
  goHome();
  hostSheetOpen.set(true);
}

/** Brain's ⋯: where its menu's popover sits on wide. */
const menuAnchor = createMenuAnchor();

/** Brain's home in the Mac's perch (mac/Perch.tsx): the perch's own header in place of the home header. */
export type PerchHome = { header: () => JSX.Element };

/**
 * Home's header (SPEC §11): the cat (the computer page), "Brain" with the computer's name under
 * it when there are several, the state control (the tray), and ⋯ (Brain's menu). 52 pt, canvas,
 * no line; the tray dims it with the page. On wide (§14.1) only "Brain" and ⋯: the left pane holds
 * the cat, the computer's name and the tray. Each part comes and goes on its own, so crossing
 * the wide gate (a resize, a turn, a fold) makes nothing else again.
 */
function HomeHeader() {
  // Mewla for Mac's window: the right pane's 46 bar, "Brain" 13.5 and a 28 ⋯ (RESULT-DESIGN-PAIRS #4).
  const mac = density() !== "phone";
  const head = headerSize();
  // The Mac's window in one column (under 1024, and the first-run window): its own header row, after the lights.
  if (mac && isMacApp()) return <If when={wide()} else={<MacColumnHeader />}><HomeHeaderRow mac={mac} head={head} /></If>;
  return <HomeHeaderRow mac={mac} head={head} />;
}

/**
 * Mewla for Mac's header in one column (m-ready, m-perm, m-noagent, w-small; win.css `.rh`): 46 high, after the
 * traffic lights (84), the 32 pet and "Brain" over this Mac's name, then the state in words (13/600, its 7 pt dot,
 * no pill) and a 28 ⋯. The cat opens the computer page, the words the tray, ⋯ Brain's menu.
 */
function MacColumnHeader() {
  const head = headerSize();
  return (
    <View testID="home-header" class="bg-background pr-3" style={{ paddingLeft: macLightsRow() > 0 ? 84 : 18, paddingTop: homeHeaderInset() }}>
      <View class="h-[46px] flex-row items-center gap-2.5">
        {/* While Brain wakes (m-waking) the row says only "Brain": the cat and the words are the page's. */}
        <If when={surface() !== "waking"} else={<Text role="header" class="text-[13.5px] leading-[17px] font-semibold">Brain</Text>}><>
        <Button testID="home-cat" accessibilityLabel={`Computer, ${current()?.name ?? ""}`} onPress={() => openPage({ name: "computer" })}
          class="min-w-0 shrink flex-row items-center gap-2 active:opacity-70">
          <HomeCat size={32} box={32} dot={false} />
          <View class="min-w-0 shrink justify-center">
            <Text role="header" class="text-[13.5px] leading-[17px] font-semibold line-clamp-1">Brain</Text>
            <If when={current()?.name}><Text testID="home-computer" class="text-[12px] leading-[15px] text-muted-foreground line-clamp-1">{current()!.name}</Text></If>
          </View>
        </Button>
        <View class="flex-1" />
        <View class="shrink-0"><StateControl onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })} capsule={{ short: () => false }} /></View>
        <Button ref={menuAnchor.ref} testID="brain-menu" accessibilityLabel="Brain's menu" onPress={() => menuOpen.set(true)}
          class="size-7 items-center justify-center rounded-lg hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
          <Icon name="more-horizontal" size={head.actionIcon} tone="soft" />
        </Button>
        </></If>
      </View>
    </View>
  );
}

function HomeHeaderRow(props: { mac: boolean; head: ReturnType<typeof headerSize> }) {
  const mac = props.mac;
  const head = props.head;
  return (
    <View testID="home-header" class={`bg-background ${mac ? "pl-[18px] pr-3" : wide() ? "px-4" : "px-2"}`} style={{ paddingTop: homeHeaderInset() }}>
      {/* The tray hangs from this row's bottom (HOME_HEADER_HEIGHT). */}
      <View class="flex-row items-center gap-1" style={{ height: mac ? 46 : HOME_HEADER_HEIGHT }}>
        <If when={wide()} else={<Button testID="home-cat" accessibilityLabel={`Computer, ${current()?.name ?? ""}`}
            onPress={() => openPage({ name: "computer" })} class="min-h-[48px] min-w-0 shrink flex-row items-center pr-2 active:opacity-70">
            <HomeCat />
            <View class="min-w-0 shrink justify-center">
              <Text role="header" class="text-title font-bold tracking-title">Brain</Text>
              <If when={current()?.name}><View class="min-w-0 flex-row items-center gap-0.5">
                  <Text testID="home-computer" class="shrink text-caption text-muted-foreground line-clamp-1">{current()!.name}</Text>
                  <Icon name="chevron-down" size={14} tone="faint" />
                </View></If>
            </View>
          </Button>}><View class="min-w-0 shrink-0 justify-center pr-1">
            <Text role="header" class={mac ? head.title : "text-title font-bold tracking-title"}>Brain</Text>
          </View></If>
        <If when={!wide()}><View class="min-w-0 shrink"><StateControl onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })} /></View></If>
        <View class="flex-1" />
        <Button ref={menuAnchor.ref} testID="brain-menu" accessibilityLabel="Brain's menu" onPress={() => menuOpen.set(true)}
          class={`${mac ? "size-7" : "size-11"} items-center justify-center rounded-full hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`}>
          <Icon name="more-horizontal" size={mac ? 16 : 22} tone="ink" />
        </Button>
      </View>
    </View>
  );
}

/**
 * Brain's capsule header on a phone (SPEC §11): the cat (the computer page), "Brain" with the computer's name
 * under it when there are two or more, the state control (the running things), and ⋯ (Brain's menu).
 * Collapsed it keeps the cat and the state control, short ("● 3" while something needs you or runs, its words
 * otherwise); both still act at once, so it has no tap that only expands, and ⋯ returns at the newest end.
 */
function BrainCapsule(props: { capsule: ReturnType<typeof createCapsule> }) {
  // With the side rail up the state control and ⋯ are the rail's, and the rail's Brain says where you are: the
  // capsule keeps the cat (the computer page) alone, a 44 circle.
  const collapsed = () => Boolean(rail()) || props.capsule.collapsed();
  const computerName = () => (servers().length > 1 ? current()?.name ?? "" : "");
  return (
    <Capsule testID="home-header" collapsed={collapsed} alone={() => Boolean(rail())} twoLines={() => Boolean(computerName())}>
      <HomeCat size={CAPSULE.cat.size} box={CAPSULE.cat.box} onPress={() => openPage({ name: "computer" })} />
      <If when={!collapsed()}><>
          <View testID="home-title" class="min-w-0 shrink justify-center" style={{ marginLeft: 2 }}>
            <Text role="header" class="font-semibold text-foreground line-clamp-1" style={{ fontSize: CAPSULE.brain.size, lineHeight: CAPSULE.brain.line }}>Brain</Text>
            <If when={computerName()}><Text testID="home-computer" class="text-muted-foreground line-clamp-1" style={{ fontSize: CAPSULE.computer.size, lineHeight: CAPSULE.computer.line }}>{computerName()}</Text></If>
          </View>
          <View class="flex-1" />
        </></If>
      <If when={!rail()}><StateControl onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })} capsule={{ short: collapsed }} /></If>
      <If when={!collapsed()}><Button ref={menuAnchor.ref} testID="brain-menu" accessibilityLabel="Brain's menu" onPress={() => menuOpen.set(true)}
          class="size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name="more-horizontal" size={22} tone="ink" />
        </Button></If>
    </Capsule>
  );
}

/**
 * Brain's thread and composer in Mewla for Mac's window (win.css `.col`): at most 700 wide, centred, 24 in from its
 * edges (the rows' own 16 and 8 more), at every width, one column or two; the header above runs the window's width.
 * Elsewhere the page's own column holds them.
 */
/** The composer files dropped on Brain's thread go to (`FileDrop`). */
type DropInto = ReturnType<typeof signal<{ attach: (incoming: IncomingFiles) => void } | null>>;

function ThreadColumn(props: { on: boolean; children: JSX.Element }) {
  // Off the Mac the children are the page's own, untouched (no view, no branch around them).
  if (!props.on) return props.children;
  return <View testID="mac-thread-column" class="w-full flex-1 self-center px-2" style={{ maxWidth: 700 }}>{props.children}</View>;
}

/** Brain's Brain host sheet (the tray's Switch executor, the computer page's Switch). */
const hostSheetOpen = signal(false);

function Body(props: { handlers: Parameters<typeof ChatTimeline>[0]["handlers"]; capsule?: { top: () => number; reading: CapsuleReading } }) {
  const openTerminal = (id: string) => openBrainTerminal(id);
  // What the page shows, as one word: the branch below is made again only when the word changes. Read as
  // conditions in the JSX, every Brain snapshot (each Work and Worker change) and every chat state made the
  // thread again: its rows all drawn anew (the whole page flashed) and the list back at its end (owner 11-13).
  // A thread is its own list: another thread (a past one, Current chat, New chat) is made anew and opens where
  // the person had read it back to, else at its end; the same thread under a restarted host is not made anew.
  const shows = computed(() => {
    const on = surface();
    // No agent on this Mac: Brain can't start, and the thread offers the installs (m-noagent).
    if (on === "host_down" && isMacApp() && brainAgentInstalls(brain()).length > 0) return "no_agent";
    if (on !== "chat") return on;
    if (brainChatUnavailable(brain())) return "unavailable";
    return chatState() === "failed" ? "failed" : `chat:${routedBrainThread() ?? brain()?.chat_thread_id ?? ""}`;
  });
  return (
    // Under a capsule the thread is padded itself; the other states start below it.
    <View class="flex-1" style={{ paddingTop: props.capsule && !shows().startsWith("chat:") ? props.capsule.top() : 0 }}>
      {/* One of these at a time, by `shows`; the thread is made again for each thread it names. */}
      <If when={shows() === "unavailable"}><BrainChatUnavailable onOpenTerminal={openTerminal} onSwitchExecutor={() => hostSheetOpen.set(true)} /></If>
      <If when={shows() === "failed"}><BrainChatFailed onOpenTerminal={openTerminal} reason={brainThread().conversation?.reason} /></If>
      <Each of={shows().startsWith("chat:") ? [shows()] : []} key={(thread) => thread}>{(thread) => {
        const rows = () => threadRows(thread().slice("chat:".length));
        return (
        // Its reading place is kept by computer and thread: made again (a hot patch to this page, a past thread and
        // back) it opens where the person had read back to, or at its end. It opens on its own thread's rows: the
        // thread shown changes a moment before its chat does, and opened on the other thread's rows it stayed short
        // of its end when its own came in (FAILURES: ThreadStability's past thread, CI 11-16).
        <ChatTimeline testID="brain-chat" items={rows} handlers={props.handlers} shown={homeShown} sent={brainSends} capsule={props.capsule} lines={updateLineRows} scope={brainThreadReadingScope(untracked(current)?.id ?? "", thread().slice("chat:".length))}
          empty={() => (
            // SPEC §16.4: nothing under 150 ms, the bottom-held placeholder, the slow line at 3 s, Try again at
            // 15 s; "What should Brain do?" only after a read that succeeded.
            <ThreadWait waiting={() => chatState() === "loading"} hasRows={() => rows().length > 0} empty={() => <If when={isMacApp()} else={<Notice title="What should Brain do?" />}><MacReady /></If>}
              reload={() => brainChat()?.reload()} reconnect={() => linkHooks.reconnect()} computer={() => current()?.name}
              failedAction={{ title: "Open terminal", onPress: () => openTerminal(brain()?.host_worker?.id ?? ""), testID: "brain-chat-open-terminal" }} />
          )}
          foot={{
            shown: () => !routedBrainThread() && (requestsHere().length > 0 || sealedHere().length > 0 || chatUpdate() !== null || providerOutage() !== null),
            render: (anchor) => (
              // An update that waits says so here, above the requests: never silent (11-17).
              <View class="gap-2">
                {/* A model provider refusing requests holds work; one line says so and clears by itself. */}
                <ProviderOutageRow />
                <ChatUpdateRow />
                <RequestStack anchor={anchor} durable={durableHere} newest={newestHere} thread={{ brain: true }} cards={requests} picked={pickedRequest} notice={(text) => notice.set(text)}
                  onSource={(card) => (card.terminal ? openBrainTerminal(card.sourceWorkerId!) : openSession(card.sourceWorkerId!))}
                  openLabel="Open" onOpenTerminal={(workerId) => (workerId === hostId() ? openBrainTerminal(workerId) : openSession(workerId))} />
              </View>
            ),
          }}
          requests={() => (routedBrainThread() ? 0 : requestsHere().length)} reveal={revealRequests} />
        );
      }}</Each>
      {/* On the Mac, `m-waking`: the waking cat and "Waking up" in the middle. */}
      <If when={shows() === "waking"}><If when={isMacApp()} else={<Notice title="Brain is starting…" />}><MacWaking /></If></If>
      <If when={shows() === "host_down"}><Notice title="Brain can't start" detail={brain()?.host_problem?.detail} /></If>
      <If when={shows() === "no_agent"}><MacNoAgent /></If>
      {/* A start before the computer answers: connecting is a wait (the placeholder), Offline only at `link.away`. */}
      <If when={shows() === "offline"}>
        <ThreadWait testID="brain-offline" waiting={() => true} empty={() => <View />} reload={() => {}}
          reconnect={() => linkHooks.reconnect()} computer={() => current()?.name} />
      </If>
    </View>
  );
}

function Composer(props: { text: ReturnType<typeof createComposerText>; editor: ReturnType<typeof createMessageEdit>; dropInto: DropInto }) {
  const editor = props.editor;
  const tools = createBrainComposer({ text: props.text, onOpenTerminal: (id) => openBrainTerminal(id), onNotice: (text) => notice.set(text), onSent: () => brainSends.set(untracked(brainSends) + 1) });
  // Pasted into the field or dropped on the thread: chips, as picked ones.
  const attach = (incoming: IncomingFiles) => void attachIncoming(tools.uploads, incoming).catch((error) => notice.set(error instanceof Error ? error.message : "Could not attach the file."));
  props.dropInto.set({ attach });
  cleanup(() => props.dropInto.set(null));
  const block = () => brainComposerBlock({ surface: surface(), snapshot: brain(), chat: chatState() });
  const pairAgain = () => tray().state.kind === "pair";
  // While Brain wakes on the Mac the composer says so (`m-waking`: "Connecting…").
  const placeholder = () => (isMacApp() && surface() === "waking" ? "Connecting…" : connection() === "connected" ? "Tell Brain" : "Sends when back online");
  return (
    <>
      <ChatComposer text={props.text} placeholder={placeholder} canSend={tools.canSend} attached={() => tools.uploads.ready().length > 0}
        disabled={() => Boolean(block()) || pairAgain()} disabledReason={() => (pairAgain() ? `Pair ${current()?.name || "your computer"} again` : block() ?? "")}
        onSend={(value) => (untracked(editor.edit) ? (void editor.send(value), false) : tools.send(value))}
        running={() => activity()?.status === "running"} stopping={() => stoppingFor() === activity()?.id} canStop={() => connection() === "connected"} onStop={() => void stop()}
        active={() => homeShown() && !trayOpen()} pickerOpen={tools.pickerOpen}
        onFiles={attach}
        leading={<ComposerPlusButton pinned={tools.pinned} connected={() => connection() === "connected"} onPress={() => tools.pinned.set(!untracked(tools.pinned))} />}
        onRecall={() => {
          const item = lastEditablePrompt(untracked(items), { connected: untracked(connection) === "connected", running: untracked(activity)?.status === "running" });
          if (item) editor.open(item);
        }}>
        <MentionPanel text={props.text.text} executors={tools.executors} onPick={(draft) => props.text.set(draft)} />
        <SlashMenu text={props.text.text} commands={tools.commands} commandsUnavailable={tools.commandsUnavailable} agentKind={tools.kind} connected={() => connection() === "connected"}
          pinned={tools.pinned} onPick={tools.pick} onAttach={tools.attach} uploading={tools.uploading} />
        <If when={tools.uploads.items().length > 0}><AttachmentTray uploads={tools.uploads} /></If>
        <If when={editor.edit()}><EditChip editor={editor} /></If>
        <If when={notice()}><Text testID="brain-toast" class="pb-1.5 text-center text-sm text-muted-foreground" onPress={() => notice.set("")}>{notice()}</Text></If>
      </ChatComposer>
      <If when={hostId()}><>
          <CodexStatusSheet open={tools.statusOpen} onClose={() => tools.statusOpen.set(false)} transport={currentTransport()!} workerId={hostId()} />
          <CodexSkillsSheet open={tools.skillsOpen} onClose={() => tools.skillsOpen.set(false)} transport={currentTransport()!} cwd={brain()?.host_worker?.cwd} onPick={tools.insertSkill} />
        </></If>
    </>
  );
}

/** Brain's menu › Browse workspace: Brain's folder, its host Session playing its media when it runs. */
export function browseWorkspace(): void {
  openPage(workspaceRoute(brainHostIdentity()));
}

export function Home(props: { perch?: PerchHome } = {}) {
  const text = createComposerText(brainDraft);
  // What's New waits while Brain's composer holds a draft.
  effect(() => brainComposing.set(text.text().trim().length > 0));
  // Brain's thread is never rewound: an edited message goes out as a new one.
  const editor = createMessageEdit({ text, brain: true, messageRewind: () => false, send: (value) => brainChat()?.send(value) });
  const handlers = {
    onUserMessage: (item: Parameters<typeof editOnTap>[0]) => {
      if (!untracked(routedBrainThread)) editOnTap(item, { editor, connected: untracked(connection) === "connected", running: untracked(activity)?.status === "running" });
    },
    // A past thread is read-only: its Work cards only read.
    get work() {
      return routedBrainThread() ? undefined : workHandlers;
    },
    workingSince: () => activity()?.started_at,
    // A reply's image (a Calendar run's result too) is a file on Brain's host.
    loadImage: (path: string, full?: boolean) => {
      const server = untracked(current);
      return server ? chatImage(currentTransport(), server.id, untracked(brain)?.host_worker?.cwd, path, full) : Promise.reject(new Error("No computer yet."));
    },
  };
  // Mewla for Mac's window: the header runs the window's width, the thread and the composer sit in the column.
  const macColumn = density() === "window";
  // The capsule header on a phone (SPEC §11); wide (an open iPhone Duo too) keeps its own header (crossing the gate switches them).
  const phone = mobile && platform.idiom === "phone";
  const capsuled = () => phone && !wide();
  const capsule = createCapsule();
  const inset = () => (rail() ? capsuleRailInset(safeArea().top) : capsuleInset(safeArea().top));
  // One more thing that needs the person, or the computer going out of reach: it expands.
  let lastState = untracked(() => tray().state);
  effect(() => {
    const now = tray().state;
    const before = lastState;
    lastState = now;
    const more = now.kind === "needs" && (now.count ?? 0) > (before.kind === "needs" ? before.count ?? 0 : 0);
    const trouble = ["connecting", "offline", "pair", "cant_start"].includes(now.kind) && now.kind !== before.kind;
    if (more || trouble) untracked(capsule.expand);
  });
  // Coming back to Brain shows it expanded.
  let wasShown = untracked(homeShown);
  effect(() => {
    const shown = homeShown();
    if (shown && !wasShown) untracked(capsule.expand);
    wasShown = shown;
  });
  // The composer under the thread, while there is one (a past thread has none): where dropped files go.
  const dropInto: DropInto = signal(null);
  return (
    <NavPage title="Brain" column="reading" frame={macColumn ? false : undefined} headerVisible={false} header={<View />} rail={[
      { id: "stop", transient: true, icon: "stop", label: stoppingFor() === activity()?.id ? "Stopping Brain" : "Stop Brain", onPress: () => void stop(), shown: () => activity()?.status === "running" && connection() === "connected" },
      { id: "brain-menu", icon: "more-horizontal", label: "Brain's menu", onPress: () => menuOpen.set(true), ref: menuAnchor.ref },
    ]}>
      <CapsulePage>
        {/* With the side rail up (an open iPhone Duo on its side, beside the pane) nothing is left for the header. */}
        <If when={!capsuled() && !rail()}><If when={props.perch} else={<HomeHeader />}>{(perch) => perch().header()}</If></If>
        <FileDrop testID="file-drop-brain" onFiles={() => dropInto()?.attach ?? null}>
          <ThreadColumn on={macColumn}>
            <If when={routedBrainThread()}><View style={{ paddingTop: capsuled() ? inset() : 0 }}><PastThread /></View></If>
            <Body handlers={handlers} capsule={phone ? { top: () => (capsuled() && !routedBrainThread() ? inset() : 0), reading: capsule.reading } : undefined} />
            <If when={!routedBrainThread()}><Composer text={text} editor={editor} dropInto={dropInto} /></If>
          </ThreadColumn>
        </FileDrop>
        <If when={capsuled()}><BrainCapsule capsule={capsule} /></If>
        <If when={!wide()}><Tray top={capsuled() ? inset : undefined} onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })} onNewSession={() => newSessionOpen.set(true)} onShowCard={showRequestCard}
          onUseExecutor={(id) => {
            const executor = (brain()?.executors ?? []).find((ref) => ref.id === id);
            if (executor) void currentBrain.hostSwitch.switchTo(executor);
          }}
          onSwitchExecutor={() => hostSheetOpen.set(true)} /></If>
        <BrainMenu open={menuOpen} onClose={() => menuOpen.set(false)} anchor={menuAnchor.node}
          onOpenTerminal={(hostWorkerId: string) => openBrainTerminal(hostWorkerId)}
          onBrowseWorkspace={browseWorkspace}
          onNewChat={() => startNewChat()}
          onNotice={(text) => notice.set(text)} />
        <BrainHostSheet open={hostSheetOpen} onClose={() => hostSheetOpen.set(false)} />
        <ResultSheet />
      </CapsulePage>
    </NavPage>
  );
}
