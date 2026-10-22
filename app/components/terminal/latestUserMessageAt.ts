/**
 * When the person last spoke in this conversation, counting a message still
 * waiting to be delivered. Watch's own events reach Brain as user input but
 * are not the person speaking.
 */
export function latestUserMessageAt(
  events: readonly { id: string; role?: string; timestamp?: string }[] | undefined,
  pending: readonly unknown[],
): number {
  let latest = 0;
  for (const event of events ?? []) {
    if (event.role !== "user" || event.id.startsWith("watch:")) continue;
    const at = event.timestamp ? Date.parse(event.timestamp) : NaN;
    if (Number.isFinite(at) && at > latest) latest = at;
  }
  return pending.length > 0 ? Number.MAX_SAFE_INTEGER : latest;
}
