import { useCallback, useEffect, useRef, useState } from "react";
import type { CodexStatus } from "../../services/codexStatus";
import { wsClient } from "../../services/websocket";

export type CodexStatusState =
  | { phase: "idle" }
  | { phase: "loading" }
  | { phase: "ready"; status: CodexStatus; fetchedAt: string }
  | { phase: "failed"; message: string; unavailable: boolean };

/**
 * Loads a chat's Codex status from the daemon, which asks Codex's own
 * app-server. A load started for another server or chat than the current one
 * is dropped, and switching either clears what was shown.
 */
export function useCodexStatus({
  serverId,
  workerId,
  connected,
}: {
  serverId: string;
  workerId: string;
  connected: boolean;
}) {
  const [state, setState] = useState<CodexStatusState>({ phase: "idle" });
  const generation = useRef(0);

  useEffect(() => {
    generation.current += 1;
    setState({ phase: "idle" });
  }, [serverId, workerId]);

  const load = useCallback(() => {
    const mine = ++generation.current;
    if (!connected || !serverId || !workerId) {
      setState({
        phase: "failed",
        message: "Not connected to the server.",
        unavailable: false,
      });
      return;
    }
    setState({ phase: "loading" });
    wsClient
      .getCodexStatus(serverId, workerId)
      .then((status) => {
        if (generation.current === mine) {
          setState({
            phase: "ready",
            status,
            fetchedAt: new Date().toISOString(),
          });
        }
      })
      .catch((error: { message?: string; code?: string }) => {
        if (generation.current === mine) {
          setState({
            phase: "failed",
            message: error?.message || "Failed to load Codex status.",
            unavailable: error?.code === "codex_status_unavailable",
          });
        }
      });
  }, [connected, serverId, workerId]);

  return { state, load };
}
