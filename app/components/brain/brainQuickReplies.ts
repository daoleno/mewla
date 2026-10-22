import type { BrainQuickReplies } from "../../store/brain";

/**
 * Whether the chips under Brain's last message still apply: they belong to a
 * question, so they go once the person has written anything after it (or is
 * writing now). A tap is the person's reply, so a delivered reply hides them.
 */
export function quickRepliesVisible(
  replies: BrainQuickReplies | undefined,
  input: { draft: string; lastUserMessageAt: number; now?: number },
): replies is BrainQuickReplies {
  if (!replies || replies.rows.length === 0) return false;
  if (input.draft.trim().length > 0) return false;
  const at = Date.parse(replies.at);
  if (!Number.isFinite(at)) return false;
  return input.lastUserMessageAt <= at;
}
