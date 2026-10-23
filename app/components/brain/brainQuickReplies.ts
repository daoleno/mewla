import type { BrainQuickReplies } from "../../store/brain";
import type { TimelineItem } from "../terminal/InterfaceTimelineItemView";

/** What the timeline needs to show the chips: the one line and what a tap does. */
export type QuickRepliesRow = {
  chips: readonly string[];
  onSelect(text: string): void;
};

/** Timeline id of the one chips row; it stays put while the chips change. */
export const QUICK_REPLIES_ITEM_ID = "quick-replies";

export type QuickRepliesTimelineItem = {
  type: "quick-replies";
  id: typeof QUICK_REPLIES_ITEM_ID;
  timestamp?: undefined;
} & QuickRepliesRow;

/**
 * Whether the chips under Brain's last message still apply: they belong to a
 * question, so they go once the person has written anything after it (or is
 * writing now). A tap is the person's reply, so a delivered reply hides them.
 * They also need that message on screen: while the chat is loading or waking
 * there is nothing for them to answer. They stay out of the way of writing:
 * hidden while the composer has focus as well as while it holds text.
 */
export function quickRepliesVisible(
  replies: BrainQuickReplies | undefined,
  input: { draft: string; focused: boolean; lastUserMessageAt: number; hasMessage: boolean },
): replies is BrainQuickReplies {
  if (!replies || replies.rows.length === 0) return false;
  if (!input.hasMessage) return false;
  if (input.focused || input.draft.trim().length > 0) return false;
  const at = Date.parse(replies.at);
  if (!Number.isFinite(at)) return false;
  return input.lastUserMessageAt <= at;
}

/** Rows are Brain's grouping; the line shown is all of them in order. */
export function quickReplyLine(replies: BrainQuickReplies): string[] {
  const seen = new Set<string>();
  return replies.rows.flat().filter((text) => {
    if (!text || seen.has(text)) return false;
    seen.add(text);
    return true;
  });
}

/**
 * Puts the chips in the flow of Brain's last message: straight after that
 * message (and the action row it carries), before anything that follows such
 * as the cat's status row. No assistant message, no chips.
 */
export function mergeQuickRepliesIntoTimeline(
  items: TimelineItem[],
  replies: QuickRepliesRow | undefined,
): TimelineItem[] {
  if (!replies || replies.chips.length === 0) return items;
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    if (item.type === "message" && item.role === "assistant") {
      return [
        ...items.slice(0, index + 1),
        { type: "quick-replies", id: QUICK_REPLIES_ITEM_ID, ...replies },
        ...items.slice(index + 1),
      ];
    }
  }
  return items;
}
