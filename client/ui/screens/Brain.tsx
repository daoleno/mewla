import { computed, untracked, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
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
import { brainNav, selectedTab } from "../shell/Shell";
import { lastEditablePrompt } from "../../core/web/composer";
import { WorkHeaderButton } from "../work/WorkPage";
import { brain, brainChat, brainDraft, brainThread, connection, current, currentTransport, openBrainThread, routedBrainThread } from "../session";

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
// A Work card opens its Work (what came back, what it needs), as the Expo app's result sheet did.
const openWorkEvent = (event: BrainWorkResultEvent) => {
  const transport = currentTransport();
  if (transport && event.unread) markWorkRead(transport, event.work_id);
  brainNav.push({ name: "workDetail", workId: event.work_id });
};
// A past thread is read-only: its Work cards and messages offer no actions (Expo's readOnly).
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

/** New chat (Brain's header, the desktop palette). */
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
      <Text testID="brain-notice" class="text-center text-lg font-semibold">{props.title}</Text>
      {props.detail ? <Text class="text-center text-base text-muted-foreground">{props.detail}</Text> : null}
    </View>
  );
}

/** A past thread a notification opened: read-only, with the way back to Brain's current chat. */
function PastThread() {
  return (
    <View testID="brain-past-thread" class="flex-row items-center gap-3 border-b border-border px-4 py-2.5">
      <View class="flex-1">
        <Text class="text-sm font-semibold">Earlier Brain thread</Text>
        <Text class="text-sm text-muted-foreground">Read-only.</Text>
      </View>
      <Button testID="brain-current-thread" onPress={() => openBrainThread(null)}>
        <Text class="text-sm text-primary">Current chat</Text>
      </Button>
    </View>
  );
}

function Body(props: { handlers: Parameters<typeof ChatTimeline>[0]["handlers"] }) {
  return (
    <View class="flex-1">
      {surface() === "chat" ? (
        <ChatTimeline testID="brain-chat" items={items} handlers={props.handlers}
          empty={() => (brainThread().loading ? <Notice title={connection() === "connected" ? "Loading…" : "The conversation shows when your computer is back"} /> : <Notice title="What should Brain do?" detail="Brain plans the work and runs the agents on your computer." />)} />
      ) : surface() === "waking" ? (
        <Notice title="Brain is starting…" />
      ) : surface() === "host_down" ? (
        <Notice title="Brain can't start" detail={brain()?.host_problem?.detail} />
      ) : surface() === "offline" ? (
        <Notice title="Your computer is offline" detail={`Mewla reconnects to ${current()?.name || "it"} on its own.`} />
      ) : (
        <Notice title="No computer" detail="Pair a computer in More → Settings." />
      )}
    </View>
  );
}

function Composer(props: { text: ReturnType<typeof createComposerText>; editor: ReturnType<typeof createMessageEdit> }) {
  const editor = props.editor;
  return (
    <ChatComposer text={props.text} placeholder="Tell Brain…" canSend={() => Boolean(brainChat())}
      onSend={(value) => (untracked(editor.edit) ? (void editor.send(value), false) : brainChat()?.send(value))}
      running={() => activity()?.status === "running"} stopping={() => stoppingFor() === activity()?.id} canStop={() => connection() === "connected"} onStop={() => void stop()}
      active={() => selectedTab() === 0 && brainNav.routes().length === 1}
      onRecall={() => {
        const item = lastEditablePrompt(untracked(items), { connected: untracked(connection) === "connected", running: untracked(activity)?.status === "running" });
        if (item) editor.open(item);
      }}>
      {connection() !== "connected" && surface() === "chat" ? (
        <Text testID="offline" class="pb-1.5 text-center text-sm text-muted-foreground">Offline · messages send when your computer is back</Text>
      ) : null}
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

export function Brain() {
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
    <NavPage title="Brain" column="reading" headerLeft={<WorkHeaderButton onPress={() => brainNav.push({ name: "work" })} />}
      headerRight={<Button testID="new-chat" onPress={() => void startNewChat()} disabled={connection() !== "connected" || startingChat()}><Text class="text-base text-primary">New chat</Text></Button>}>
      <View class="flex-1 bg-background">
        {routedBrainThread() ? <PastThread /> : null}
        <Body handlers={handlers} />
        {routedBrainThread() ? null : <Composer text={text} editor={editor} />}
      </View>
    </NavPage>
  );
}
