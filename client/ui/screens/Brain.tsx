import { computed, untracked, signal, Text, View } from "nsh";
import { Button, NavPage } from "nsh/base";
import { Composer as ChatComposer, createComposerText } from "../chat/Composer";
import { createMessageEdit, EditChip, showMessageMenu } from "../chat/messageEdit";
import { finalReplyIdsOf } from "../../core/timeline/model";
import { ChoiceCard, CHOICE_GONE_NOTICE } from "../chat/ChoiceCard";
import { ChatTimeline } from "../chat/Timeline";
import { answerChoice, stopTurn } from "../../core/conversation/turn";
import { visiblePendingChoice } from "../../core/timeline/claudeChoiceModel";
import { brainSurface, newBrainChat } from "../../core/brain";
import { createChatTimeline } from "../../core/conversation/timeline";
import { brainNav } from "../shell/Shell";
import { WorkHeaderButton } from "../work/WorkPage";
import { brain, brainChat, brainDraft, brainThread, connection, current, currentTransport } from "../session";

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
const items = computed(() => timeline(brainThread(), { onPendingAction: (id, action) => untracked(brainChat)?.actOnFailed(id, action) }));

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

async function startNewChat(): Promise<void> {
  const transport = currentTransport();
  if (!transport || startingChat()) return;
  startingChat.set(true);
  try {
    await newBrainChat(transport);
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
      running={() => activity()?.status === "running"} stopping={() => stoppingFor() === activity()?.id} canStop={() => connection() === "connected"} onStop={() => void stop()}>
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
    onUserMessage: (item: Parameters<typeof showMessageMenu>[0]) =>
      void showMessageMenu(item, { editor, connected: untracked(connection) === "connected", running: untracked(activity)?.status === "running" }),
    isFinalReply: (id: string) => finalReplies().has(id),
    workingSince: () => activity()?.started_at,
  };
  return (
    <NavPage title="Brain" headerLeft={<WorkHeaderButton onPress={() => brainNav.push({ name: "work" })} />}
      headerRight={<Button testID="new-chat" onPress={() => void startNewChat()} disabled={connection() !== "connected" || startingChat()}><Text class="text-base text-primary">New chat</Text></Button>}>
      <View class="flex-1 bg-background">
        <Body handlers={handlers} />
        <Composer text={text} editor={editor} />
      </View>
    </NavPage>
  );
}
