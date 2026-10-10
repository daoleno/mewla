import { useEffect, useState } from "react";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkerServerSummary } from "../../store/workers";
import {
  CONNECTION_STALL_MS,
  connectionAttention,
  type ConnectionAttention,
} from "./connectionAttention";

/**
 * Whether the current server has been connecting for longer than a short
 * reconnect. The menu footer and Brain's cat say "Reconnecting" from the same
 * moment.
 */
export function useConnectionStalled(): boolean {
  const { currentServer } = useCurrentServer();
  const { serverConnections } = useWorkerServerSummary();
  const serverId = currentServer?.id;
  const connection = serverId ? serverConnections[serverId] || "offline" : "offline";
  const [stalled, setStalled] = useState(false);
  useEffect(() => {
    setStalled(false);
    if (connection !== "connecting") return;
    const timer = setTimeout(() => setStalled(true), CONNECTION_STALL_MS);
    return () => clearTimeout(timer);
  }, [connection, serverId]);
  return stalled;
}

/** The current server's connection signal (see `connectionAttention`). */
export function useConnectionAttention(): ConnectionAttention {
  const { currentServer } = useCurrentServer();
  const { serverConnections, serverConnectionIssues } = useWorkerServerSummary();
  const serverId = currentServer?.id;
  const connection = serverId ? serverConnections[serverId] || "offline" : "offline";
  const issue = serverId ? serverConnectionIssues[serverId] ?? null : null;
  const stalled = useConnectionStalled();
  return connectionAttention({
    hasServer: Boolean(serverId),
    connection,
    issue,
    stalled,
  });
}
