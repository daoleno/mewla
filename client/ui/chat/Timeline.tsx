import { List, Text, View } from "nsh";
import { Button, TextView } from "nsh/base";
import { isWorkingRow } from "../../core/conversation/timeline";
import type { ActivityTimelineItem } from "../../core/timeline/activityTypes";
import type { ComputerMessageTimelineItem } from "../../core/timeline/computerMessageCardModel";
import type { TaskNotificationTimelineItem } from "../../core/timeline/taskNotificationCardModel";
import type { BrainWorkEventTimelineItem, MessageTimelineItem, PlanTimelineItem, TimelineItem } from "../../core/timeline/types";
import { Icon } from "../icons/Icon";
import { PlainText } from "./PlainText";
import { ActivityRow, ComputerMessageCard, createExpansion, PlanRow, TaskNotificationCard, WorkEventCard, type Expansion } from "./TimelineRows";

function UserMessage(props: { item: () => MessageTimelineItem }) {
  const it = props.item;
  const failed = () => it().pending && it().pendingLifecycle === "failed";
  const waiting = () => it().pending && !failed();
  return (
    <View class="max-w-[85%] items-end gap-1 self-end">
      <View class={failed() ? "rounded-2xl border border-destructive px-3.5 py-2.5" : waiting() ? "rounded-2xl bg-secondary px-3.5 py-2.5 opacity-60" : "rounded-2xl bg-secondary px-3.5 py-2.5"}>
        {it().body ? <PlainText text={it().body} class="text-base" /> : null}
        {it().attachments.map((file) => (
          <View class="flex-row items-center gap-1.5 pt-1">
            <Icon name="document" size={14} />
            <Text class="text-sm text-muted-foreground line-clamp-1">{file.name}</Text>
          </View>
        ))}
      </View>
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
export function TimelineRow(props: { item: () => TimelineItem; expansion: Expansion }) {
  const it = props.item;
  const as = <T extends TimelineItem["type"]>(type: T) => () => it() as Extract<TimelineItem, { type: T }>;
  return (
    <View class="py-1">
      {it().type === "message" && (it() as MessageTimelineItem).role === "user" ? (
        <View class="px-4"><UserMessage item={as("message")} /></View>
      ) : it().type === "message" ? (
        <View class="px-4">
          <TextView markdown={(it() as MessageTimelineItem).body} options={{ streaming: Boolean((it() as MessageTimelineItem).streaming) }} selectable class="text-base" />
        </View>
      ) : isWorkingRow(it()) ? (
        <Text testID="working" class="px-4 text-base text-muted-foreground">Working…</Text>
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
export function ChatTimeline(props: { testID: string; items: () => TimelineItem[]; empty: () => unknown }) {
  const expansion = createExpansion();
  return (
    <List testID={props.testID} class="flex-1" each={props.items()} keyExtractor={(item) => item.id}
      maintainVisibleContentPosition={{ autoscrollToBottomThreshold: 80 }} keyboardDismissMode="interactive" contentClass="py-3"
      empty={props.empty() as never}>
      {(item) => <TimelineRow item={item} expansion={expansion} />}
    </List>
  );
}
