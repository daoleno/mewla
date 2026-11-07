import { useCallback, useEffect, useRef, useState } from "react";
import { browserRequest } from "../services/browser";
import { useCurrentServer } from "./currentServer";

/**
 * Labs → Virtual screen, read from and written to the current computer: it
 * decides whether that computer's Workers get a browser on its virtual screen
 * and whether Screen shows them. A server switch drops what was known and
 * reads the new computer, so the answer never belongs to another one.
 */
export function useVirtualScreenLabs(): { enabled: boolean; ready: boolean; set(on: boolean): Promise<void> } {
  const { currentServer } = useCurrentServer();
  const [state, setState] = useState<{ server: string; enabled: boolean } | null>(null);
  const server = useRef(currentServer);
  server.current = currentServer;

  useEffect(() => {
    setState(null);
    if (!currentServer) return;
    const controller = new AbortController();
    browserRequest(currentServer, { action: "settings" }, controller.signal)
      .then((result) => { if (!controller.signal.aborted) setState({ server: currentServer.id, enabled: result.virtual_screen === true }); })
      // A computer without Browser (older Mewla, not Linux) simply has the switch off.
      .catch(() => { if (!controller.signal.aborted) setState({ server: currentServer.id, enabled: false }); });
    return () => controller.abort();
  }, [currentServer?.id]);

  const set = useCallback(async (on: boolean) => {
    const target = server.current;
    if (!target) return;
    const result = await browserRequest(target, { action: "settings", enabled: on });
    if (server.current?.id === target.id) setState({ server: target.id, enabled: result.virtual_screen === true });
  }, []);

  const enabled = !!currentServer && state?.server === currentServer.id && state.enabled;
  return { enabled, ready: !!currentServer && state?.server === currentServer.id, set };
}
