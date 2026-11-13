import { isEditablePrompt } from "../timeline/messageActionsModel";
import type { MessageTimelineItem } from "../timeline/types";

/**
 * ↑ in an empty composer edits the person's last message: the newest of their
 * prompts that Edit would offer (sent, text only, in a connected idle chat).
 * Null when there is none, or when the newest prompt cannot be edited: ↑ never
 * reaches past it to an older one.
 */
export function lastEditablePrompt(
  items: readonly { type: string }[],
  env: { connected: boolean; running: boolean },
): MessageTimelineItem | null {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    if (item.type !== "message") continue;
    const message = item as MessageTimelineItem;
    if (message.role !== "user") continue;
    const subject = { role: message.role, body: message.body, hasAttachments: message.attachments.length > 0, pending: Boolean(message.pending) };
    const editable = isEditablePrompt(subject, { running: env.running, connected: env.connected });
    return editable ? message : null;
  }
  return null;
}
