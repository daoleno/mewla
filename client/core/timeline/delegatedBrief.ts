/**
 * A delegated Worker's brief (BRAIN-LOOK item 6): the first user_message of a Worker's Session is the
 * brief Mewla handed it, "Execute: <task>\n\nMewla lifecycle protocol:\n…". The chat shows the task alone;
 * the protocol is Mewla's, the same for every Worker, and never shown.
 */

/** The `origin` the daemon gives a delegated brief (`work.ConversationOriginDelegated`). */
export const DELEGATED_ORIGIN = "delegated";

const PROTOCOL_LINE = /^Mewla lifecycle protocol:/m;
const INTENT_PREFIX = /^Execute:[ \t]*/;

/**
 * The task of a delegated brief, or null when the message is not one. An empty string is a brief
 * with no task of its own (only the protocol): it shows nothing.
 */
export function delegatedTask(event: { body?: string; origin?: string }): string | null {
  const body = (event.body ?? "").trim();
  const marked = event.origin === DELEGATED_ORIGIN;
  // Legacy fallback for daemons from before `origin: "delegated"`: only the strict shape Mewla itself
  // writes, the typed intent first and the protocol on a line of its own. Remove once every paired
  // computer runs a daemon that marks the brief.
  const legacy = !marked && INTENT_PREFIX.test(body) && PROTOCOL_LINE.test(body);
  if (!marked && !legacy) return null;
  const withoutIntent = body.replace(INTENT_PREFIX, "");
  const protocol = withoutIntent.search(PROTOCOL_LINE);
  return (protocol < 0 ? withoutIntent : withoutIntent.slice(0, protocol)).trim();
}
