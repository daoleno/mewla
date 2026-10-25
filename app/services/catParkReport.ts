import { wsClient } from "./websocket";

/** Said once the link is back if it is down now: it is often why the sync failed. */
const REPORT_WAIT_MS = 5 * 60_000;

/**
 * One line in the daemon's log about a cat sync that failed, or that only
 * worked on a later run. The daemon cannot see the phone's requests, so
 * without this a house that never arrives leaves nothing to look at.
 */
export function reportCatSync(serverId: string, event: "cat_sync_failed" | "cat_sync_recovered", detail: string): void {
  if (wsClient.isConnected(serverId)) {
    wsClient.reportDiagnostic(serverId, event, detail);
    return;
  }
  const onConnected = (payload: { serverId?: string }) => {
    if (payload.serverId !== serverId) return;
    clearTimeout(timer);
    wsClient.off("connected", onConnected);
    wsClient.reportDiagnostic(serverId, event, detail);
  };
  const timer = setTimeout(() => wsClient.off("connected", onConnected), REPORT_WAIT_MS);
  wsClient.on("connected", onConnected);
}
