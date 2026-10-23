import type { BrainQuickReplies } from "../../store/brain";

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
