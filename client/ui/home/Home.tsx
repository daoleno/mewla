import { computed, safeArea, untracked, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { NavPage } from "../shell/NavPage";
import { Composer as ChatComposer, createComposerText } from "../chat/Composer";
import { createMessageEdit, EditChip, showMessageMenu } from "../chat/messageEdit";
import { finalReplyIdsOf } from "../../core/timeline/model";
import { ChoiceCard, CHOICE_GONE_NOTICE } from "../chat/ChoiceCard";
import { ChatTimeline } from "../chat/Timeline";
import { chatImage } from "../chat/assets";
import { answerChoice, stopTurn } from "../../core/conversation/turn";
import { visiblePendingChoice } from "../../core/timeline/claudeChoiceModel";
import { brainSurface, newBrainChat } from "../../core/brain";
import { createChatTimeline } from "../../core/conversation/timeline";
import type { BrainWorkResultEvent } from "../../core/timeline/brainWorkEvent";
import type { BrainCurrentWork } from "../../core/timeline/types";
import { markWorkRead } from "../../core/work/brainWork";
import { goHome, homeShown, openBrainTerminal, openPage, openWorkDetail, trayOpen } from "../shell/nav";
import { lastEditablePrompt } from "../../core/web/composer";
import { workspaceRoute } from "../more/files/routes";
import { brain, brainChat, brainDraft, brainThread, connection, current, currentTransport, openBrainThread, routedBrainThread, servers } from "../session";
import { Icon } from "../kit/Icon";
import { HomeCat } from "./Cat";
import { StateControl } from "../tray/StateControl";
import { Tray } from "../tray/Tray";
import { BrainMenu } from "../brain/BrainMenu";
import { newSessionOpen } from "../screens/NewSession";

const startingChat = signal(false);
/** Questions the computer said have passed; their card stays hidden even if the transcript lags. */
const goneChoices = signal<ReadonlySet<string>>(new Set());
const notice = signal("");
const pendingChoice = computed(() => visiblePendingChoice(brainThread().conversation?.events, goneChoices()));
const pendingChoiceKey = computed(() => pendingChoice()?.call_id ?? "");
const activity = computed(() => brainThread().conversation?.activity);
/** The activity a Stop was sent for: the button says "Stopping…" until that turn ends. */
const stoppingFor = signal("");
const timeline = createChatTimeline("brain");
// A Work card opens its Work detail (what came back, what it needs).
const openWorkEvent = (event: BrainWorkResultEvent) => {
  const transport = currentTransport();
  if (transport && event.unread) markWorkRead(transport, event.work_id);
  openWorkDetail(event.work_id);
};
// A past thread is read-only: its Work cards and messages offer no actions.
const items = computed(() => timeline(brainThread(), {
  onPendingAction: routedBrainThread() ? undefined : (id, action) => untracked(brainChat)?.actOnFailed(id, action),
  onWorkEvent: routedBrainThread() ? undefined : openWorkEvent,
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

/** New chat (Brain's menu, the desktop palette). */
export async function startNewChat(): Promise<void> {
  const transport = currentTransport();
  if (!transport || startingChat()) return;
  startingChat.set(true);
  try {
    await newBrainChat(transport);
    // A past thread was showing: the new chat is what the person asked for.
    openBrainThread(null);
  } catch {
    // Offline or refused: the current chat stays; the button can be pressed again.
  } finally {
    startingChat.set(false);
  }
}

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

/** Brain's menu is open (the header's ⋯). */
const menuOpen = signal(false);

/** Home with Brain's menu open, for its Brain host sheet (the computer page's "Claude is not signed in" › Switch). */
export function openBrainHost(): void {
  goHome();
  menuOpen.set(true);
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

function Body(props: { handlers: Parameters<typeof ChatTimeline>[0]["handlers"] }) {
  return (
    <View class="flex-1">
      {surface() === "chat" ? (
        <ChatTimeline testID="brain-chat" items={items} handlers={props.handlers} shown={homeShown}
          empty={() => (brainThread().loading ? <Notice title={connection() === "connected" ? "Loading…" : "The conversation shows when your computer is back"} /> : <Notice title="What should Brain do?" detail="Brain plans the work and runs the agents on your computer." />)} />
      ) : surface() === "waking" ? (
        <Notice title="Brain is starting…" />
      ) : surface() === "host_down" ? (
        <Notice title="Brain can't start" detail={brain()?.host_problem?.detail} />
      ) : surface() === "offline" ? (
        <Notice title="Your computer is offline" detail={`Mewla reconnects to ${current()?.name || "it"} on its own.`} />
      ) : null}
    </View>
  );
}

function Composer(props: { text: ReturnType<typeof createComposerText>; editor: ReturnType<typeof createMessageEdit> }) {
  const editor = props.editor;
  return (
    <ChatComposer text={props.text} placeholder={connection() === "connected" ? "Tell Brain…" : "Sends when back online"} canSend={() => Boolean(brainChat())}
      onSend={(value) => (untracked(editor.edit) ? (void editor.send(value), false) : brainChat()?.send(value))}
      running={() => activity()?.status === "running"} stopping={() => stoppingFor() === activity()?.id} canStop={() => connection() === "connected"} onStop={() => void stop()}
      active={() => homeShown() && !trayOpen()}
      onRecall={() => {
        const item = lastEditablePrompt(untracked(items), { connected: untracked(connection) === "connected", running: untracked(activity)?.status === "running" });
        if (item) editor.open(item);
      }}>
      {editor.edit() ? <EditChip editor={editor} /> : null}
      {pendingChoiceKey() ? (
        <ChoiceCard choice={untracked(pendingChoice)!.choice} connected={() => connection() === "connected"}
          submit={(answers) => {
            const transport = currentTransport();
            const callId = untracked(pendingChoiceKey);
            return transport ? answerChoice(transport, hostId(), callId, answers) : Promise.reject(new Error("Your computer is offline."));
          }}
          onGone={() => {
            goneChoices.set(new Set([...goneChoices(), untracked(pendingChoiceKey)]));
            notice.set(CHOICE_GONE_NOTICE);
          }} />
      ) : null}
      {notice() ? <Text testID="brain-toast" class="pb-1.5 text-center text-sm text-muted-foreground" onPress={() => notice.set("")}>{notice()}</Text> : null}
    </ChatComposer>
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
        <Tray onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })} onNewSession={() => newSessionOpen.set(true)} onShowCard={() => {}} />
        <BrainMenu open={menuOpen} onClose={() => menuOpen.set(false)}
          onOpenTerminal={(hostWorkerId: string) => openBrainTerminal(hostWorkerId)}
          onBrowseWorkspace={browseWorkspace}
          onNewChat={() => startNewChat()} />
      </View>
    </NavPage>
  );
}
