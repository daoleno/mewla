import { computed, effect, openUrl, platform, safeArea, untracked, signal, Text, View } from "nsh";
import { CAPSULE, capsuleInset } from "../../core/shell/capsule";
import { Capsule, CapsulePage, createCapsule, type CapsuleReading } from "../shell/Capsule";
import { progressSteps } from "../../core/sessions/progress";
import { Button } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { NavPage } from "../shell/NavPage";
import { Composer as ChatComposer, createComposerText } from "../chat/Composer";
import { createMessageEdit, EditChip, editOnTap } from "../chat/messageEdit";
import { durableAnswered, RequestStack, threadRequests, threadSealed } from "../chat/RequestCard";
import { ChatTimeline } from "../chat/Timeline";
import { ThreadWait } from "../chat/ThreadWait";
import { brainHostIdentity } from "../chat/links";
import { chatImage } from "../chat/assets";
import { stopTurn } from "../../core/conversation/turn";
import { brainSurface, newBrainChat } from "../../core/brain";
import { createChatTimeline } from "../../core/conversation/timeline";
import type { BrainWorkResultEvent } from "../../core/timeline/brainWorkEvent";
import type { BrainCurrentWork } from "../../core/timeline/types";
import { markWorkRead } from "../../core/work/brainWork";
import { goHome, homeShown, openBrainTerminal, openPage, openSession, openWorkDetail, trayOpen } from "../shell/nav";
import { lastEditablePrompt } from "../../core/web/composer";
import { workspaceRoute } from "../more/files/routes";
import { brain, brainChat, brainDraft, brainThread, connection, current, currentTransport, openBrainThread, routedBrainThread, servers, workers } from "../session";
import { ResultActions, type WorkCardHandlers } from "../chat/TimelineRows";
import type { ResultAction } from "../../core/work/types";
import type { WorkCardModel } from "../../core/timeline/workCardModel";
import { sessionRowModel } from "../../core/sessions/listRow";
import { workWorkerLines } from "../../core/work/workers";
import { presentWorker } from "../../core/sessions/workerPresentation";
import { Sheet } from "../kit/Sheet";
import { PillButton } from "../kit/kit";
import { clock } from "../calendar/clock";
import { Icon } from "../kit/Icon";
import { HomeCat } from "./Cat";
import { StateControl } from "../tray/StateControl";
import { HOME_HEADER_HEIGHT, homeHeaderInset, Tray } from "../tray/Tray";
import { BrainChatFailed, BrainChatUnavailable, BrainHostSheet, BrainMenu, brainChatState, brainChatUnavailable, brainComposerBlock, createBrainComposer, currentBrain, MentionPanel } from "../brain";
import { newSessionOpen } from "../screens/NewSession";
import { ComposerPlusButton, SlashMenu } from "../session/SlashMenu";
import { CodexSkillsSheet } from "../session/CodexSkillsSheet";
import { CodexStatusSheet } from "../session/CodexStatusSheet";
import { AttachmentTray } from "../more/files/Pages";
import { tray } from "../tray/state";
import { wide } from "../shell/layout";
import { createMenuAnchor } from "../kit/Menu";

const startingChat = signal(false);
const notice = signal("");
/** Brain's thread's open requests (SPEC §1.1): Brain's own and those of the Sessions working for its Work. */
const requests = threadRequests({ brain: true });
/** The request the person (or a tray row) opened in the stack; none opens the oldest. */
const pickedRequest = signal("");
/** Bumped to bring the open request into view. */
const revealRequests = signal(0);

/** A tray row of a request in Brain's thread (the tray has closed): the thread scrolls to that card, open. */
export function showRequestCard(key: string): void {
  pickedRequest.set(key);
  if (!homeShown()) goHome();
  revealRequests.set(revealRequests() + 1);
}
const activity = computed(() => brainThread().conversation?.activity);
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
    return { kind: row.kind, flavor: row.flavor, name: presentWorker(worker).typeLabel, progress: progressSteps(worker.phase) ?? undefined };
  },
  onTitle: (workId) => openWorkDetail(workId),
  workers: (rows) => workWorkerLines(rows, new Map(workers().map((w) => [w.id, w] as const))),
  onSession: (sessionId) => openSession(sessionId),
  onLine: (model) => resultSheet.set(model),
  onAction: runResultAction,
};
// A past thread is read-only: its Work cards and messages offer no actions.
const items = computed(() => timeline(brainThread(), {
  onPendingAction: routedBrainThread() ? undefined : (id, action) => untracked(brainChat)?.actOnFailed(id, action),
  onWorkEvent: routedBrainThread() ? undefined : openWorkEvent,
  openSessionIds: new Set(workers().map((w) => w.id)),
  currentWork: brain()?.current_work as readonly BrainCurrentWork[] | undefined,
}));
const durableHere = durableAnswered(items);
const sealedHere = threadSealed({ brain: true }, durableHere);

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

function Notice(props: { title: string; detail?: string }) {
  return (
    <View class="flex-1 items-center justify-center gap-2 p-8">
      <Text testID="brain-notice" class="text-center text-title font-semibold tracking-title">{props.title}</Text>
      {props.detail ? <Text class="text-center text-sub text-muted-foreground">{props.detail}</Text> : null}
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
      {model() ? (
        <View class="gap-3 px-4">
          <Text class="text-sub text-muted-foreground">{[agent()?.name, model()!.state, when()].filter(Boolean).join(" · ")}</Text>
          {model()!.summary ? <View selectable><Text class="text-body">{model()!.summary}</Text></View> : null}
          {model()!.actions.length ? <ResultActions actions={model()!.actions} onPress={(action) => runResultAction(untracked(resultSheet)!, action)} /> : null}
          {model()!.sessionId && workers().some((w) => w.id === model()!.sessionId) ? (
            <PillButton testID="work-sheet-open-session" title="Open session" onPress={() => {
              const id = untracked(resultSheet)!.sessionId!;
              close();
              openSession(id);
            }} />
          ) : null}
        </View>
      ) : null}
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

/**
 * Home's header (SPEC §11): the cat (the computer page), "Brain" with the computer's name under
 * it when there are several, the state control (the tray), and ⋯ (Brain's menu). 52 pt, canvas,
 * no line; the tray dims it with the page. On wide (§14.1) only "Brain" and ⋯: the left pane holds
 * the cat, the computer's name and the tray. Each part comes and goes on its own, so crossing
 * 1024 makes nothing else again.
 */
function HomeHeader() {
  return (
    <View testID="home-header" class={`bg-background ${wide() ? "px-4" : "px-2"}`} style={{ paddingTop: homeHeaderInset() }}>
      {/* The tray hangs from this row's bottom (HOME_HEADER_HEIGHT). */}
      <View class="flex-row items-center gap-1" style={{ height: HOME_HEADER_HEIGHT }}>
        {wide() ? (
          <View class="min-w-0 shrink-0 justify-center pr-1">
            <Text role="header" class="text-title font-bold tracking-title">Brain</Text>
          </View>
        ) : (
          // Where I am: the cat, "Brain" and the computer's name are one target that opens the computer page,
          // and the name wears the state control's chevron, so the header reads as two controls of one kind.
          <Button testID="home-cat" accessibilityLabel={`Computer, ${current()?.name ?? ""}`}
            onPress={() => openPage({ name: "computer" })} class="min-h-[48px] min-w-0 shrink flex-row items-center pr-2 active:opacity-70">
            <HomeCat />
            <View class="min-w-0 shrink justify-center">
              <Text role="header" class="text-title font-bold tracking-title">Brain</Text>
              {current()?.name ? (
                <View class="min-w-0 flex-row items-center gap-0.5">
                  <Text testID="home-computer" class="shrink text-caption text-muted-foreground line-clamp-1">{current()!.name}</Text>
                  <Icon name="chevron-down" size={14} tone="faint" />
                </View>
              ) : null}
            </View>
          </Button>
        )}
        {wide() ? null : <View class="min-w-0 shrink"><StateControl onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })} /></View>}
        <View class="flex-1" />
        <Button ref={menuAnchor.ref} testID="brain-menu" accessibilityLabel="Brain's menu" onPress={() => menuOpen.set(true)}
          class="size-11 items-center justify-center rounded-full hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
          <Icon name="more-horizontal" size={22} tone="ink" />
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
  const collapsed = props.capsule.collapsed;
  const computerName = () => (servers().length > 1 ? current()?.name ?? "" : "");
  return (
    <Capsule testID="home-header" collapsed={collapsed} twoLines={() => Boolean(computerName())}>
      <HomeCat size={CAPSULE.cat.size} box={CAPSULE.cat.box} onPress={() => openPage({ name: "computer" })} />
      {collapsed() ? null : (
        <>
          <View testID="home-title" class="min-w-0 shrink justify-center" style={{ marginLeft: 2 }}>
            <Text role="header" class="font-semibold text-foreground line-clamp-1" style={{ fontSize: CAPSULE.brain.size, lineHeight: CAPSULE.brain.line }}>Brain</Text>
            {computerName() ? (
              <Text testID="home-computer" class="text-muted-foreground line-clamp-1" style={{ fontSize: CAPSULE.computer.size, lineHeight: CAPSULE.computer.line }}>{computerName()}</Text>
            ) : null}
          </View>
          <View class="flex-1" />
        </>
      )}
      <StateControl onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })} capsule={{ short: collapsed }} />
      {collapsed() ? null : (
        <Button ref={menuAnchor.ref} testID="brain-menu" accessibilityLabel="Brain's menu" onPress={() => menuOpen.set(true)}
          class="size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name="more-horizontal" size={22} tone="ink" />
        </Button>
      )}
    </Capsule>
  );
}

/** Brain's Brain host sheet (the tray's Switch executor, the computer page's Switch). */
const hostSheetOpen = signal(false);

function Body(props: { handlers: Parameters<typeof ChatTimeline>[0]["handlers"]; capsule?: { top: () => number; reading: CapsuleReading } }) {
  const openTerminal = (id: string) => openBrainTerminal(id);
  // What the page shows, as one word: the branch below is made again only when the word changes. Read as
  // conditions in the JSX, every Brain snapshot (each Work and Worker change) and every chat state made the
  // thread again: its rows all drawn anew (the whole page flashed) and the list back at its end (owner 11-13).
  // A thread is its own list: another thread (a past one, Current chat, New chat) is made anew and opens at
  // its end; the same thread under a restarted host is not.
  const shows = computed(() => {
    const on = surface();
    if (on !== "chat") return on;
    if (brainChatUnavailable(brain())) return "unavailable";
    return chatState() === "failed" ? "failed" : `chat:${routedBrainThread() ?? brain()?.chat_thread_id ?? ""}`;
  });
  return (
    // Under a capsule the thread is padded itself; the other states start below it.
    <View class="flex-1" style={{ paddingTop: props.capsule && !shows().startsWith("chat:") ? props.capsule.top() : 0 }}>
      {shows() === "unavailable" ? (
        <BrainChatUnavailable onOpenTerminal={openTerminal} onSwitchExecutor={() => hostSheetOpen.set(true)} />
      ) : shows() === "failed" ? (
        <BrainChatFailed onOpenTerminal={openTerminal} />
      ) : shows().startsWith("chat:") ? (
        <ChatTimeline testID="brain-chat" items={items} handlers={props.handlers} shown={homeShown} capsule={props.capsule}
          empty={() => (
            // SPEC §16.4: nothing under 150 ms, the bottom-held placeholder, the slow line at 3 s, Try again at
            // 15 s; "What should Brain do?" only after a read that succeeded.
            <ThreadWait waiting={() => chatState() === "loading"} hasRows={() => items().length > 0} empty={() => <Notice title="What should Brain do?" />}
              reload={() => brainChat()?.reload()} reconnect={() => currentTransport()?.resume()} computer={() => current()?.name}
              failedAction={{ title: "Open terminal", onPress: () => openTerminal(brain()?.host_worker?.id ?? ""), testID: "brain-chat-open-terminal" }} />
          )}
          foot={{
            shown: () => !routedBrainThread() && (requests().length > 0 || sealedHere().length > 0),
            render: (anchor) => (
              <RequestStack anchor={anchor} durable={durableHere} thread={{ brain: true }} cards={requests} picked={pickedRequest} notice={(text) => notice.set(text)}
                onSource={(card) => (card.terminal ? openBrainTerminal(card.sourceWorkerId!) : openSession(card.sourceWorkerId!))}
                onOpenTerminal={(workerId) => (workerId === hostId() ? openBrainTerminal(workerId) : openSession(workerId))} />
            ),
          }}
          requests={() => (routedBrainThread() ? 0 : requests().length)} reveal={revealRequests} />
      ) : shows() === "waking" ? (
        <Notice title="Brain is starting…" />
      ) : shows() === "host_down" ? (
        <Notice title="Brain can't start" />
      ) : shows() === "offline" ? (
        // A start before the computer answers: connecting is a wait (the placeholder), Offline only at `link.away`.
        <ThreadWait testID="brain-offline" waiting={() => true} empty={() => <View />} reload={() => {}}
          reconnect={() => currentTransport()?.resume()} computer={() => current()?.name} />
      ) : null}
    </View>
  );
}

function Composer(props: { text: ReturnType<typeof createComposerText>; editor: ReturnType<typeof createMessageEdit> }) {
  const editor = props.editor;
  const tools = createBrainComposer({ text: props.text, onOpenTerminal: (id) => openBrainTerminal(id), onNotice: (text) => notice.set(text) });
  const block = () => brainComposerBlock({ surface: surface(), snapshot: brain(), chat: chatState() });
  const pairAgain = () => tray().state.kind === "pair";
  const placeholder = () => (connection() === "connected" ? "Tell Brain" : "Sends when back online");
  return (
    <>
      <ChatComposer text={props.text} placeholder={placeholder} canSend={tools.canSend} attached={() => tools.uploads.ready().length > 0}
        disabled={() => Boolean(block()) || pairAgain()} disabledReason={() => (pairAgain() ? `Pair ${current()?.name || "your computer"} again` : block() ?? "")}
        onSend={(value) => (untracked(editor.edit) ? (void editor.send(value), false) : tools.send(value))}
        running={() => activity()?.status === "running"} stopping={() => stoppingFor() === activity()?.id} canStop={() => connection() === "connected"} onStop={() => void stop()}
        active={() => homeShown() && !trayOpen()} pickerOpen={tools.pickerOpen}
        onFiles={(files) => {
          try {
            tools.uploads.add(files);
          } catch (error) {
            notice.set(error instanceof Error ? error.message : "Could not attach the file.");
          }
        }}
        leading={<ComposerPlusButton pinned={tools.pinned} connected={() => connection() === "connected"} onPress={() => tools.pinned.set(!untracked(tools.pinned))} />}
        onRecall={() => {
          const item = lastEditablePrompt(untracked(items), { connected: untracked(connection) === "connected", running: untracked(activity)?.status === "running" });
          if (item) editor.open(item);
        }}>
        <MentionPanel text={props.text.text} executors={tools.executors} onPick={(draft) => props.text.set(draft)} />
        <SlashMenu text={props.text.text} commands={tools.commands} commandsUnavailable={tools.commandsUnavailable} agentKind={tools.kind} connected={() => connection() === "connected"}
          pinned={tools.pinned} onPick={tools.pick} onAttach={tools.attach} uploading={tools.uploading} />
        {tools.uploads.items().length > 0 ? <AttachmentTray uploads={tools.uploads} /> : null}
        {editor.edit() ? <EditChip editor={editor} /> : null}
        {notice() ? <Text testID="brain-toast" class="pb-1.5 text-center text-sm text-muted-foreground" onPress={() => notice.set("")}>{notice()}</Text> : null}
      </ChatComposer>
      {hostId() ? (
        <>
          <CodexStatusSheet open={tools.statusOpen} onClose={() => tools.statusOpen.set(false)} transport={currentTransport()!} workerId={hostId()} />
          <CodexSkillsSheet open={tools.skillsOpen} onClose={() => tools.skillsOpen.set(false)} transport={currentTransport()!} cwd={brain()?.host_worker?.cwd} onPick={tools.insertSkill} />
        </>
      ) : null}
    </>
  );
}

/** Brain's menu › Browse workspace: Brain's folder, its host Session playing its media when it runs. */
export function browseWorkspace(): void {
  openPage(workspaceRoute(brainHostIdentity()));
}

export function Home() {
  const text = createComposerText(brainDraft);
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
    loadImage: (path: string) => {
      const server = untracked(current);
      return server ? chatImage(currentTransport(), server.id, untracked(brain)?.host_worker?.cwd, path) : Promise.resolve(null);
    },
  };
  // The capsule header on a phone (SPEC §11); wide keeps its own header (crossing 1024 switches them).
  const phone = mobile && platform.idiom === "phone";
  const capsuled = () => phone && !wide();
  const capsule = createCapsule();
  const inset = () => capsuleInset(safeArea().top);
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
  return (
    <NavPage title="Brain" column="reading" headerVisible={false} header={<View />}>
      <CapsulePage>
        {capsuled() ? null : <HomeHeader />}
        {routedBrainThread() ? <View style={{ paddingTop: capsuled() ? inset() : 0 }}><PastThread /></View> : null}
        <Body handlers={handlers} capsule={phone ? { top: () => (capsuled() && !routedBrainThread() ? inset() : 0), reading: capsule.reading } : undefined} />
        {routedBrainThread() ? null : <Composer text={text} editor={editor} />}
        {capsuled() ? <BrainCapsule capsule={capsule} /> : null}
        {wide() ? null : <Tray top={capsuled() ? inset : undefined} onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })} onNewSession={() => newSessionOpen.set(true)} onShowCard={showRequestCard}
          onUseExecutor={(id) => {
            const executor = (brain()?.executors ?? []).find((ref) => ref.id === id);
            if (executor) void currentBrain.hostSwitch.switchTo(executor);
          }}
          onSwitchExecutor={() => hostSheetOpen.set(true)} />}
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
