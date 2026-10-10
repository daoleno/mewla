import type { ConnectionState } from "../store/workers";

/** Why the client reported a disconnect. */
export type DisconnectReason = "intentional" | "transport_closed";

export type DisconnectLifecycleDecision = {
  /** Connection presentation while this disconnect is handled. */
  connectionState: ConnectionState;
  /** Whether Brain/Work (and similar) server caches should be wiped. */
  clearServerCaches: boolean;
};

export type ConnectedReadModelClient = {
  listWorkItems(serverId: string): void;
  listWorkerSessions(serverId: string): void;
  requestBrainSnapshot(serverId: string): void;
  listCalendarItems(serverId: string): void;
};

/**
 * Reconnect rebuilds read models with fresh requests. Nothing sent while the
 * socket was unavailable is retained or replayed by the transport.
 */
export function createConnectedReadRefreshHandler(
  client: ConnectedReadModelClient,
) {
  return ({ serverId }: { serverId: string }) => {
    client.listWorkItems(serverId);
    client.listWorkerSessions(serverId);
    client.requestBrainSnapshot(serverId);
    client.listCalendarItems(serverId);
  };
}

/**
 * App backgrounding and transient socket close are expected mobile lifecycle
 * events. Only an intentional disconnect (user disable/remove, tear-down)
 * should drop connected presentation and wipe cached server content.
 */
export function decideDisconnectLifecycle(
  reason: DisconnectReason | string | undefined | null,
): DisconnectLifecycleDecision {
  if (reason === "intentional") {
    return {
      connectionState: "offline",
      clearServerCaches: true,
    };
  }
  return {
    connectionState: "connecting",
    clearServerCaches: false,
  };
}

export function isIntentionalDisconnect(
  reason: DisconnectReason | string | undefined | null,
): boolean {
  return reason === "intentional";
}

/**
 * Brain empty/offline card is only for cold start or intentional wipe.
 * Cached hydrated Brain stays visible while transport resumes.
 */
export function shouldShowBrainLoadingState({
  hydrated,
  hasHostWorker,
}: {
  hydrated: boolean;
  hasHostWorker: boolean;
}): boolean {
  return !(hydrated && hasHostWorker);
}

/**
 * What the Brain screen shows. While Brain is still waking (connecting, or
 * connected before its first snapshot) the chat is already there, loading,
 * with the composer saying "Connecting…"; only no computer or an offline
 * one gets the status card.
 */
export function brainScreenSurface({
  hasServer,
  connection,
  hydrated,
  hasHostWorker,
  structuredEvents,
}: {
  hasServer: boolean;
  connection: ConnectionState;
  hydrated: boolean;
  hasHostWorker: boolean;
  structuredEvents: boolean;
}): "chat" | "waking" | "status" | "unavailable" {
  if (!hasServer) return "status";
  if (!shouldShowBrainLoadingState({ hydrated, hasHostWorker })) {
    return structuredEvents ? "chat" : "unavailable";
  }
  return connection === "offline" ? "status" : "waking";
}
