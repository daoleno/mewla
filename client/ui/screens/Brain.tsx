import { effect, keyboardFrame, List, untracked, signal, Text, View } from "nsh";
import { Button, Input, NavPage, TextView } from "nsh/base";
import { PlainText } from "../chat/PlainText";
import { brainSurface, newBrainChat } from "../../core/brain";
import { chatRows, type ChatRow } from "../../core/conversation/rows";
import { belowSections, brainNav } from "../shell/Shell";
import { WorkHeaderButton } from "../work/WorkPage";
import { brain, brainChat, brainDraft, brainThread, connection, current, currentTransport } from "../session";

const draft = signal("");
const draftFor = signal("");
const startingChat = signal(false);

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

function Bubble(props: { text: string; faded?: boolean; failed?: boolean }) {
  return (
    <View class={props.failed ? "rounded-2xl border border-destructive px-3.5 py-2.5" : props.faded ? "rounded-2xl bg-secondary px-3.5 py-2.5 opacity-60" : "rounded-2xl bg-secondary px-3.5 py-2.5"}>
      <PlainText text={props.text} class="text-base" />
    </View>
  );
}

/** One recycled cell: everything is read from `row()` inside JSX, so a reused cell shows its new row. */
function Row(props: { row: () => ChatRow }) {
  const user = () => { const r = props.row(); return r.kind === "user" ? r : null; };
  const reply = () => { const r = props.row(); return r.kind === "assistant" ? r : null; };
  const pending = () => { const r = props.row(); return r.kind === "pending" ? r : null; };
  return (
    <View class="px-4 py-1.5">
      {user() ? (
        <View class="max-w-[85%] self-end"><Bubble text={user()!.text} /></View>
      ) : reply() ? (
        <TextView markdown={reply()!.text} options={{ streaming: reply()!.partial }} selectable class="text-base" />
      ) : pending() ? (
        <View class="max-w-[85%] items-end gap-1 self-end">
          <Bubble text={pending()!.text} faded={!pending()!.failed} failed={pending()!.failed} />
          {pending()!.note ? <Text class="text-sm text-muted-foreground">{pending()!.note}</Text> : null}
          {pending()!.failed ? (
            <View class="flex-row gap-3">
              <Button testID="retry" onPress={() => brainChat()?.actOnFailed(pending()!.rowId, "retry")}><Text class="text-sm font-semibold text-primary">Send again</Text></Button>
              <Button testID="discard" onPress={() => brainChat()?.actOnFailed(pending()!.rowId, "discard")}><Text class="text-sm text-muted-foreground">Delete</Text></Button>
            </View>
          ) : null}
        </View>
      ) : (
        <Text testID="working" class="text-base text-muted-foreground">Working…</Text>
      )}
    </View>
  );
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
        <List testID="brain-chat" class="flex-1" each={chatRows(brainThread())} keyExtractor={(row) => row.key}
          maintainVisibleContentPosition={{ autoscrollToBottomThreshold: 80 }} keyboardDismissMode="interactive" contentClass="py-3"
          empty={brainThread().loading ? <Notice title={connection() === "connected" ? "Loading…" : "The conversation shows when your computer is back"} /> : <Notice title="What should Brain do?" detail="Brain plans the work and runs the agents on your computer." />}>
          {(row) => <Row row={row} />}
        </List>
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
      <View class="flex-row items-end gap-2">
        <Input testID="composer" multiline value={draft()} placeholder="Tell Brain…" class="max-h-40 flex-1 rounded-2xl border border-border bg-card px-3.5 py-2.5 text-base"
          onChange={({ value }) => {
            draft.set(value);
            brainDraft()?.save(value);
          }} />
        <Button testID="send" onPress={send} disabled={!brainChat() || !draft().trim()} class="rounded-full bg-primary px-4 py-2.5 active:opacity-80">
          <Text class="text-base font-semibold text-primary-foreground">Send</Text>
        </Button>
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
