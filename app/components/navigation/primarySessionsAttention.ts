/**
 * True when a Session on the current server is waiting on you. It drives the
 * seal dot on "Sessions", one of the three places vermilion is allowed.
 */
export function sessionsNeedYou(
  workers: readonly { serverId: string; needs_attention?: boolean }[],
  currentServerId: string | null | undefined,
): boolean {
  if (!currentServerId) return false;
  return workers.some(
    (worker) => worker.serverId === currentServerId && worker.needs_attention === true,
  );
}
