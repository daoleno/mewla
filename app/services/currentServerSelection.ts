import type { StoredServer } from "./storage";

export function enqueueCurrentServerPersistence(
  pending: Promise<void>,
  write: () => Promise<void>,
): { result: Promise<void>; tail: Promise<void> } {
  const result = pending.then(write, write);
  return {
    result,
    tail: result.then(
      () => undefined,
      () => undefined,
    ),
  };
}

/**
 * Resolve the one persisted server identity. Array order is used only when no
 * identity was ever persisted (installations that predate the current-server
 * key, and the first pairing). A persisted identity whose server is gone is
 * not replaced by another computer: there is then no current server, and the
 * UI shows its no-server state until the person picks one.
 */
export function resolveCurrentServerId(
  servers: readonly Pick<StoredServer, "id">[],
  persistedServerId: string | null | undefined,
): string | null {
  const persisted = persistedServerId?.trim() || "";
  if (persisted) {
    return servers.some((server) => server.id === persisted) ? persisted : null;
  }
  return servers[0]?.id ?? null;
}

/**
 * Whether a resolved identity should be written back. "No current server" is
 * never written: clearing the key would make the next refresh read it as a
 * pre-key installation and pick the first computer after all.
 */
export function shouldPersistResolvedServerId(
  resolvedId: string | null,
  persistedServerId: string | null | undefined,
): resolvedId is string {
  return resolvedId !== null && resolvedId !== (persistedServerId?.trim() || null);
}

export function selectCurrentServer<T extends Pick<StoredServer, "id">>(
  servers: readonly T[],
  currentServerId: string | null | undefined,
): T | null {
  if (!currentServerId) {
    return null;
  }
  return servers.find((server) => server.id === currentServerId) ?? null;
}

export function selectCurrentServerItems<T extends { serverId: string }>(
  items: readonly T[],
  currentServerId: string | null | undefined,
): T[] {
  if (!currentServerId) return [];
  return items.filter((item) => item.serverId === currentServerId);
}
