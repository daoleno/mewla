import { computed, effect, keyboardFrame, untracked, signal, Text, View } from "nsh";
import { Button, Input, NavPage } from "nsh/base";
import { ChoiceCard, CHOICE_GONE_NOTICE } from "../chat/ChoiceCard";
import { ChatTimeline } from "../chat/Timeline";
import { answerChoice, stopTurn } from "../../core/conversation/turn";
import { visiblePendingChoice } from "../../core/timeline/claudeChoiceModel";
import { brainSurface, newBrainChat } from "../../core/brain";
import { createChatTimeline } from "../../core/conversation/timeline";
import { belowSections, brainNav } from "../shell/Shell";
import { WorkHeaderButton } from "../work/WorkPage";
import { brain, brainChat, brainDraft, brainThread, connection, current, currentTransport } from "../session";

const draft = signal("");
const draftFor = signal("");
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

/**
 * Brings the composer to the thread's draft, once per thread (typing owns it
 * afterwards). What was typed before the thread was known stays, as its draft.
 */
function syncDraft(): void {
  const current = brainDraft();
  const key = current?.cacheKey ?? "";
  if (untracked(draftFor) === key) return;
  const typedBefore = untracked(draftFor) === "" ? untracked(draft) : "";
  draftFor.set(key);
  if (!current) return draft.set("");
  if (typedBefore) return current.save(typedBefore);
  draft.set("");
  void current.load().then((value) => untracked(brainDraft) === current && !untracked(draft) && draft.set(value));
}

function surface() {
  return brainSurface({ hasServer: Boolean(current()), connected: connection() === "connected", snapshot: brain() });
}

function send(): void {
  const chat = brainChat();
  const text = draft();
  if (!chat || !text.trim()) return;
  chat.send(text);
  draft.set("");
  brainDraft()?.save("");
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

function Body() {
  return (
    <View class="flex-1">
      {surface() === "chat" ? (
        <ChatTimeline testID="brain-chat" items={items}
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

function Composer() {
  effect(syncDraft);
  return (
    <View class="border-t border-border bg-background px-3 py-2" style={{ marginBottom: Math.max(0, keyboardFrame().height - belowSections()) }}>
      {connection() !== "connected" && surface() === "chat" ? (
        <Text testID="offline" class="pb-1.5 text-center text-sm text-muted-foreground">Offline · messages send when your computer is back</Text>
      ) : null}
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
      <View class="flex-row items-end gap-2">
        <Input testID="composer" multiline value={draft()} placeholder="Tell Brain…" class="max-h-40 flex-1 rounded-2xl border border-border bg-card px-3.5 py-2.5 text-base"
          onChange={({ value }) => {
            draft.set(value);
            brainDraft()?.save(value);
          }} />
{activity()?.status === "running" && !draft().trim() ? (
          <Button testID="stop" onPress={() => void stop()} disabled={stoppingFor() === activity()?.id || connection() !== "connected"} class="rounded-full border border-border px-4 py-2.5 active:bg-accent">
            <Text class="text-base font-semibold">{stoppingFor() === activity()?.id ? "Stopping…" : "Stop"}</Text>
          </Button>
        ) : (
          <Button testID="send" onPress={send} disabled={!brainChat() || !draft().trim()} class="rounded-full bg-primary px-4 py-2.5 active:opacity-80">
            <Text class="text-base font-semibold text-primary-foreground">Send</Text>
          </Button>
        )}
      </View>
    </View>
  );
}

export function Brain() {
  return (
    <NavPage title="Brain" headerLeft={<WorkHeaderButton onPress={() => brainNav.push({ name: "work" })} />}
      headerRight={<Button testID="new-chat" onPress={() => void startNewChat()} disabled={connection() !== "connected" || startingChat()}><Text class="text-base text-primary">New chat</Text></Button>}>
      <View class="flex-1 bg-background">
        <Body />
        <Composer />
      </View>
    </NavPage>
  );
}
