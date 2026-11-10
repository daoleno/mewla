import { awaitingChoiceAnswer } from "../timeline/claudeChoiceModel";
import { foldBrainToolRows } from "../timeline/brainToolRows";
import { createToolRunFolder } from "../timeline/foldToolRuns";
import type { BrainWorkResultEvent } from "../timeline/brainWorkEvent";
import {
  attachBrainWorkEventActions,
  mergePendingUserMessagesIntoTimeline,
  mergeRunningActivityIntoTimeline,
  PROVIDER_ACTIVITY_ITEM_PREFIX,
} from "../timeline/model";
import { projectTimeline, type TimelineProjectionCache } from "../timeline/projectTimeline";
import { timelineItemsSemanticEqual } from "../timeline/timelineItemsSemanticEqual";
import type { BrainCurrentWork, TimelineItem } from "../timeline/types";
import type { CodexConversationEvent } from "./conversation";
import type { PendingUserMessageAction } from "./pendingUserMessages";
import type { InterfaceChatThreadState } from "./thread";

export type ChatTimelineOptions = {
  onPendingAction?: (id: string, action: PendingUserMessageAction) => void;
  onWorkEvent?: (event: BrainWorkResultEvent, canOpenSession: boolean) => void;
  openSessionIds?: ReadonlySet<string>;
  currentWork?: readonly BrainCurrentWork[];
};

const NO_EVENTS: CodexConversationEvent[] = [];

/**
 * A chat's rows, from its thread state: the projected events (messages, tools,
 * plans, task and Work cards), tool runs folded (Brain keeps its "Worked"
 * fold, a Session folds finished runs into one summary row), "Working" while a
 * reply runs (not while a question waits: its card says what is needed), and
 * what the person sent that the computer has not shown yet.
 *
 * One per chat: it keeps the projection cache, so a streamed reply re-projects
 * only its own message, and returns the previous item object for every row
 * that did not change, so a list redraws only the rows that did.
 */
export function createChatTimeline(kind: "brain" | "session") {
  let projected: { events: CodexConversationEvent[]; items: TimelineItem[]; cache: TimelineProjectionCache | null } = {
    events: NO_EVENTS,
    items: [],
    cache: null,
  };
  const fold = kind === "brain" ? foldBrainToolRows : createToolRunFolder();
  let previous: { items: TimelineItem[]; byId: Map<string, TimelineItem> } = { items: [], byId: new Map() };

  return (state: InterfaceChatThreadState, options: ChatTimelineOptions = {}): TimelineItem[] => {
    const events = state.conversation?.events ?? NO_EVENTS;
    if (events !== projected.events) {
      const result = projectTimeline(events, projected.cache);
      projected = { events, items: result.items, cache: result.cache };
    }
    let items = attachBrainWorkEventActions(projected.items, options.onWorkEvent, options.openSessionIds, options.currentWork);
    items = fold(items);
    if (!awaitingChoiceAnswer(events)) items = mergeRunningActivityIntoTimeline(items, state.conversation?.activity);
    items = mergePendingUserMessagesIntoTimeline(items, state.pendingUserMessages, options.onPendingAction);

    const byId = new Map<string, TimelineItem>();
    let changed = items.length !== previous.items.length;
    const stable = items.map((item, index) => {
      const before = previous.byId.get(item.id);
      const kept = before && timelineItemsSemanticEqual(before, item) ? before : item;
      if (previous.items[index] !== kept) changed = true;
      byId.set(item.id, kept);
      return kept;
    });
    if (!changed) return previous.items;
    previous = { items: stable, byId };
    return stable;
  };
}

/** The "Working" row the timeline adds while a reply runs. */
export function isWorkingRow(item: TimelineItem): boolean {
  return item.id.startsWith(PROVIDER_ACTIVITY_ITEM_PREFIX);
}
