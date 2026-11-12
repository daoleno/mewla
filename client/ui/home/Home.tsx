import { computed, safeArea, untracked, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { NavPage } from "../shell/NavPage";
import { Composer as ChatComposer, createComposerText } from "../chat/Composer";
import { createMessageEdit, EditChip, showMessageMenu } from "../chat/messageEdit";
import { finalReplyIdsOf } from "../../core/timeline/model";
import { RequestStack, threadRequests, threadSealed } from "../chat/RequestCard";
import { ChatTimeline } from "../chat/Timeline";
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
import type { WorkCardHandlers } from "../chat/TimelineRows";
import type { WorkCardModel } from "../../core/timeline/workCardModel";
import { sessionRowModel } from "../../core/sessions/listRow";
import { presentWorker } from "../../core/sessions/workerPresentation";
import { Sheet } from "../kit/Sheet";
import { PillButton } from "../kit/kit";
import { clock } from "../calendar/clock";
import { Icon } from "../kit/Icon";
import { HomeCat } from "./Cat";
import { StateControl } from "../tray/StateControl";
import { Tray } from "../tray/Tray";
import { BrainChatFailed, BrainChatUnavailable, BrainHostSheet, BrainMenu, brainChatState, brainChatUnavailable, brainComposerBlock, createBrainComposer, currentBrain, MentionPanel } from "../brain";
import { newSessionOpen } from "../screens/NewSession";
import { ComposerPlusButton, SlashMenu } from "../session/SlashMenu";
import { CodexSkillsSheet } from "../session/CodexSkillsSheet";
import { CodexStatusSheet } from "../session/CodexStatusSheet";
import { AttachmentTray } from "../more/files/Pages";
import { tray } from "../tray/state";

const startingChat = signal(false);
const notice = signal("");
/** Brain's thread's open requests (SPEC §1.1): Brain's own and those of the Sessions working for its Work. */
const requests = threadRequests({ brain: true });
const sealedHere = threadSealed({ brain: true });
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
const workHandlers: WorkCardHandlers = {
  agentOf: (sessionId) => {
    const worker = sessionId ? workers().find((w) => w.id === sessionId) : undefined;
    if (!worker) return null;
    const row = sessionRowModel(worker);
    return { kind: row.kind, flavor: row.flavor, name: presentWorker(worker).typeLabel };
  },
  onTitle: (workId) => openWorkDetail(workId),
  onNextStep: (model, text) => {
    untracked(brainChat)?.send(text);
    readWork(model.workId);
  },
  onLine: (model) => resultSheet.set(model),
};
// A past thread is read-only: its Work cards and messages offer no actions.
const items = computed(() => timeline(brainThread(), {
  onPendingAction: routedBrainThread() ? undefined : (id, action) => untracked(brainChat)?.actOnFailed(id, action),
  onWorkEvent: routedBrainThread() ? undefined : openWorkEvent,
  openSessionIds: new Set(workers().map((w) => w.id)),
  currentWork: brain()?.current_work as readonly BrainCurrentWork[] | undefined,
}));

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
          {model()!.nextStep ? (
            <PillButton testID="work-sheet-next-step" kind="ink" title={model()!.nextStep!} onPress={() => {
              const shown = untracked(resultSheet)!;
              workHandlers.onNextStep(shown, shown.nextStep!);
              close();
            }} />
          ) : null}
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

/**
 * Home's header (SPEC §11): the cat (the computer page), "Brain" with the computer's name under
 * it when there are several, the state control (the tray), and ⋯ (Brain's menu). 52 pt, canvas,
 * no line; the tray dims it with the page.
 */
function HomeHeader() {
  return (
    <View testID="home-header" class="bg-background px-2" style={{ paddingTop: mobile ? safeArea().top : 0 }}>
      <View class="h-[52px] flex-row items-center gap-1">
        <HomeCat onPress={() => openPage({ name: "computer" })} />
        <View class="min-w-0 shrink-0 justify-center pr-1">
          <Text role="header" class="text-title font-bold tracking-title">Brain</Text>
          {servers().length > 1 ? <Text testID="home-computer" class="text-caption text-muted-foreground line-clamp-1">{current()?.name ?? ""}</Text> : null}
        </View>
        <View class="min-w-0 shrink"><StateControl onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })} /></View>
        <View class="flex-1" />
        <Button testID="brain-menu" accessibilityLabel="Brain's menu" onPress={() => menuOpen.set(true)} class="size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name="more-horizontal" size={22} tone="ink" />
        </Button>
      </View>
    </View>
  );
}

/** Brain's Brain host sheet (the tray's Switch executor, the computer page's Switch). */
const hostSheetOpen = signal(false);

function Body(props: { handlers: Parameters<typeof ChatTimeline>[0]["handlers"] }) {
  const openTerminal = (id: string) => openBrainTerminal(id);
  return (
    <View class="flex-1">
      {surface() === "chat" && brainChatUnavailable(brain()) ? (
        <BrainChatUnavailable onOpenTerminal={openTerminal} onSwitchExecutor={() => hostSheetOpen.set(true)} />
      ) : surface() === "chat" && chatState() === "failed" ? (
        <BrainChatFailed onOpenTerminal={openTerminal} />
      ) : surface() === "chat" ? (
        <ChatTimeline testID="brain-chat" items={items} handlers={props.handlers} shown={homeShown}
          empty={() => (chatState() === "loading" ? <Notice title="Loading…" /> : <Notice title="What should Brain do?" />)}
          foot={{
            shown: () => !routedBrainThread() && (requests().length > 0 || sealedHere().length > 0),
            render: () => (
              <RequestStack thread={{ brain: true }} cards={requests} picked={pickedRequest} notice={(text) => notice.set(text)}
                onSource={(card) => (card.terminal ? openBrainTerminal(card.sourceWorkerId!) : openSession(card.sourceWorkerId!))}
                onOpenTerminal={(workerId) => (workerId === hostId() ? openBrainTerminal(workerId) : openSession(workerId))} />
            ),
          }}
          requests={() => (routedBrainThread() ? 0 : requests().length)} reveal={revealRequests} />
      ) : surface() === "waking" ? (
        <Notice title="Brain is starting…" />
      ) : surface() === "host_down" ? (
        <Notice title="Brain can't start" />
      ) : surface() === "offline" ? (
        <Notice title={`${current()?.name || "Your computer"} is offline`} />
      ) : null}
    </View>
  );
}

function Composer(props: { text: ReturnType<typeof createComposerText>; editor: ReturnType<typeof createMessageEdit> }) {
  const editor = props.editor;
  const tools = createBrainComposer({ text: props.text, onOpenTerminal: (id) => openBrainTerminal(id), onNotice: (text) => notice.set(text) });
  const block = () => brainComposerBlock({ surface: surface(), snapshot: brain(), chat: chatState() });
  const pairAgain = () => tray().state.kind === "pair";
  const placeholder = () => (connection() === "connected" ? "Tell Brain…" : "Sends when back online");
  return (
    <>
      <ChatComposer text={props.text} placeholder={placeholder} canSend={tools.canSend}
        disabled={() => Boolean(block()) || pairAgain()} disabledReason={() => (pairAgain() ? `Pair ${current()?.name || "your computer"} again` : block() ?? "")}
        onSend={(value) => (untracked(editor.edit) ? (void editor.send(value), false) : tools.send(value))}
        running={() => activity()?.status === "running"} stopping={() => stoppingFor() === activity()?.id} canStop={() => connection() === "connected"} onStop={() => void stop()}
        active={() => homeShown() && !trayOpen()} pickerOpen={tools.pickerOpen}
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
function browseWorkspace(): void {
  const host = untracked(brain)?.host_worker;
  const identity = host?.id && host.process_id && host.started_at ? { workerId: host.id, processId: host.process_id, startedAt: host.started_at } : null;
  openPage(workspaceRoute(identity));
}

export function Home() {
  const text = createComposerText(brainDraft);
  // Brain's thread is never rewound: an edited message goes out as a new one.
  const editor = createMessageEdit({ text, brain: true, messageRewind: () => false, send: (value) => brainChat()?.send(value) });
  const finalReplies = computed(() => finalReplyIdsOf(brainThread().conversation?.events ?? []));
  const handlers = {
    onUserMessage: (item: Parameters<typeof showMessageMenu>[0]) => {
      if (!untracked(routedBrainThread)) void showMessageMenu(item, { editor, connected: untracked(connection) === "connected", running: untracked(activity)?.status === "running" });
    },
    isFinalReply: (id: string) => finalReplies().has(id),
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
  return (
    <NavPage title="Brain" column="reading" headerVisible={false} header={<View />}>
      <View class="flex-1 bg-background">
        <HomeHeader />
        {routedBrainThread() ? <PastThread /> : null}
        <Body handlers={handlers} />
        {routedBrainThread() ? null : <Composer text={text} editor={editor} />}
        <Tray onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })} onNewSession={() => newSessionOpen.set(true)} onShowCard={showRequestCard}
          onUseExecutor={(id) => {
            const executor = (brain()?.executors ?? []).find((ref) => ref.id === id);
            if (executor) void currentBrain.hostSwitch.switchTo(executor);
          }}
          onSwitchExecutor={() => hostSheetOpen.set(true)} />
        <BrainMenu open={menuOpen} onClose={() => menuOpen.set(false)}
          onOpenTerminal={(hostWorkerId: string) => openBrainTerminal(hostWorkerId)}
          onBrowseWorkspace={browseWorkspace}
          onNewChat={() => startNewChat()}
          onNotice={(text) => notice.set(text)} />
        <BrainHostSheet open={hostSheetOpen} onClose={() => hostSheetOpen.set(false)} />
        <ResultSheet />
      </View>
    </NavPage>
  );
}
