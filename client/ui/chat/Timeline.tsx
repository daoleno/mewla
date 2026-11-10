import { cleanup, Clipboard, List, signal, Text, View } from "nsh";
import { providerActivityElapsedLabels } from "../../core/timeline/providerActivityElapsed";
import { Button, Sheet, TextView } from "nsh/base";
import { isWorkingRow } from "../../core/conversation/timeline";
import type { ActivityTimelineItem } from "../../core/timeline/activityTypes";
import type { ComputerMessageTimelineItem } from "../../core/timeline/computerMessageCardModel";
import type { TaskNotificationTimelineItem } from "../../core/timeline/taskNotificationCardModel";
import type { BrainWorkEventTimelineItem, MessageTimelineItem, PlanTimelineItem, TimelineItem } from "../../core/timeline/types";
import { Icon } from "../icons/Icon";
import { PlainText } from "./PlainText";
import { selectingText } from "./messageEdit";
import { ActivityRow, ComputerMessageCard, createExpansion, PlanRow, TaskNotificationCard, WorkEventCard, type Expansion } from "./TimelineRows";

export type MessageHandlers = {
  /** A tap on one of the person's own messages: its menu (Copy, Edit). */
  onUserMessage?: (item: MessageTimelineItem) => void;
  /** A turn's final reply gets a Copy button under it. */
  isFinalReply?: (id: string) => boolean;
  /** When the running turn started (the provider's own clock): "Working · 1:05". */
  workingSince?: () => string | undefined;
};

/** "Working" with the turn's elapsed time, ticking each second while it shows. */
function WorkingRow(props: { since: () => string | undefined }) {
  const now = signal(Date.now());
  const timer = setInterval(() => now.set(Date.now()), 1000);
  cleanup(() => clearInterval(timer));
  const label = () => providerActivityElapsedLabels({ startedAt: props.since(), nowMs: now(), active: true }).visual;
  return <Text testID="working" class="px-4 text-base text-muted-foreground">{label() ? `Working · ${label()}` : "Working…"}</Text>;
}

function UserMessage(props: { item: () => MessageTimelineItem; handlers: MessageHandlers }) {
  const it = props.item;
  const failed = () => it().pending && it().pendingLifecycle === "failed";
  const waiting = () => it().pending && !failed();
  return (
    <View class="max-w-[85%] items-end gap-1 self-end">
      <Button testID="user-message" onPress={() => !it().pending && props.handlers.onUserMessage?.(it())} class={failed() ? "rounded-2xl border border-destructive px-3.5 py-2.5" : waiting() ? "rounded-2xl bg-secondary px-3.5 py-2.5 opacity-60" : "rounded-2xl bg-secondary px-3.5 py-2.5"}>
        {it().body ? <Text class="text-base">{it().body}</Text> : null}
        {it().attachments.map((file) => (
          <View class="flex-row items-center gap-1.5 pt-1">
            <Icon name="document" size={14} />
            <Text class="text-sm text-muted-foreground line-clamp-1">{file.name}</Text>
          </View>
        ))}
      </Button>
      {waiting() ? <Icon name="time" size={12} /> : null}
      {failed() ? (
        <View class="flex-row items-center gap-3">
          <Text class="text-sm text-destructive">Not sent</Text>
          <Button testID="retry" onPress={() => it().onRetryPending?.()}><Text class="text-sm font-semibold text-primary">Send again</Text></Button>
          <Button testID="discard" onPress={() => it().onDiscardPending?.()}><Text class="text-sm text-muted-foreground">Delete</Text></Button>
        </View>
      ) : null}
    </View>
  );
}

/**
 * One recycled cell. Every branch reads `item()` inside JSX, so a reused cell
 * shows the item it was given next; per-row state (open or not) lives in
 * `expansion`, by id.
 */
export function TimelineRow(props: { item: () => TimelineItem; expansion: Expansion; handlers: MessageHandlers }) {
  const it = props.item;
  const as = <T extends TimelineItem["type"]>(type: T) => () => it() as Extract<TimelineItem, { type: T }>;
  return (
    <View class="py-1">
      {it().type === "message" && (it() as MessageTimelineItem).role === "user" ? (
        <View class="px-4"><UserMessage item={as("message")} handlers={props.handlers} /></View>
      ) : it().type === "message" ? (
        <View class="px-4">
          <TextView markdown={(it() as MessageTimelineItem).body} options={{ streaming: Boolean((it() as MessageTimelineItem).streaming) }} selectable class="text-base" />
          {!(it() as MessageTimelineItem).streaming && props.handlers.isFinalReply?.(it().id) ? (
            <Button testID="copy-reply" onPress={() => void Clipboard.writeText((it() as MessageTimelineItem).body.replace(/\s+$/u, "")).catch(() => {})}
              class="mt-1 self-start rounded-lg p-1.5 active:bg-accent">
              <Icon name="copy" size={16} />
            </Button>
          ) : null}
        </View>
      ) : isWorkingRow(it()) ? (
        <WorkingRow since={() => props.handlers.workingSince?.()} />
      ) : it().type === "activity" ? (
        <ActivityRow item={as("activity") as () => ActivityTimelineItem} expansion={props.expansion} />
      ) : it().type === "plan" ? (
        <PlanRow item={as("plan") as () => PlanTimelineItem} />
      ) : it().type === "task-notification" ? (
        <TaskNotificationCard item={as("task-notification") as () => TaskNotificationTimelineItem} expansion={props.expansion} />
      ) : it().type === "computer-message" ? (
        <ComputerMessageCard item={as("computer-message") as () => ComputerMessageTimelineItem} expansion={props.expansion} />
      ) : it().type === "brain-work-event" ? (
        <WorkEventCard item={as("brain-work-event") as () => BrainWorkEventTimelineItem} />
      ) : null}
    </View>
  );
}

/** A chat's rows, kept at the newest one while the person reads at the bottom. */
/** Select Text: the message in a sheet, where the system's selection works on it. */
function SelectTextSheet() {
  return (
    <Sheet open={selectingText() !== ""} onOpenChange={({ open }) => !open && selectingText.set("")} closeOnSwipe
      popupClass="rounded-t-2xl bg-background px-4 pt-4" backdropClass="bg-black/40">
      <View class="gap-3 pb-8">
        <Text class="text-lg font-semibold">Select Text</Text>
        <PlainText text={selectingText()} class="text-base" />
      </View>
    </Sheet>
  );
}

export function ChatTimeline(props: { testID: string; items: () => TimelineItem[]; empty: () => unknown; handlers?: MessageHandlers }) {
  const handlers = props.handlers ?? {};
  const expansion = createExpansion();
  return (
    <View class="flex-1">
    <SelectTextSheet />
    <List testID={props.testID} class="flex-1" each={props.items()} keyExtractor={(item) => item.id}
      maintainVisibleContentPosition={{ autoscrollToBottomThreshold: 80 }} keyboardDismissMode="interactive" contentClass="py-3"
      empty={props.empty() as never}>
      {(item) => <TimelineRow item={item} expansion={expansion} handlers={handlers} />}
    </List>
    </View>
  );
}
