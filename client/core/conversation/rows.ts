import type { InterfaceChatThreadState } from "./thread";

/** One row of a chat as the screen draws it. */
export type ChatRow =
  | { key: string; kind: "user"; text: string }
  | { key: string; kind: "assistant"; text: string; partial: boolean }
  /** The person's message the computer has not echoed yet; `failed` rows offer Send again / Delete. */
  | { key: string; kind: "pending"; text: string; rowId: string; failed: boolean; note?: string }
  | { key: string; kind: "working" };

/**
 * The rows of a chat: messages in order, then what the person sent that the
 * computer has not shown yet, then "working" while a reply is under way.
 * The single place the timeline projection (tools, plans, Work cards) plugs in.
 */
export function chatRows(state: InterfaceChatThreadState): ChatRow[] {
  const rows: ChatRow[] = [];
  for (const event of state.conversation?.events ?? []) {
    const text = event.body?.trim();
    if (!text) continue;
    if (event.kind === "user_message") rows.push({ key: event.id, kind: "user", text });
    else if (event.kind === "assistant_message") rows.push({ key: event.id, kind: "assistant", text, partial: Boolean(event.partial) });
  }
  for (const message of state.pendingUserMessages) {
    rows.push({
      key: message.id,
      kind: "pending",
      text: message.body,
      rowId: message.id,
      failed: message.lifecycle === "failed",
      note: message.lifecycle === "pending" ? undefined : message.failureMessage,
    });
  }
  const last = rows.at(-1);
  const replying = last?.kind === "assistant" && last.partial;
  if (state.conversation?.activity?.status === "running" && !replying) rows.push({ key: "working", kind: "working" });
  return rows;
}
