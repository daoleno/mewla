import { useCallback, useEffect, useRef, useState } from "react";
import { AppState } from "react-native";
import { DisplayRequestError, listDisplays, type AgentDisplay } from "../../services/display";
import { screenRequest, type ScreenAgent } from "../../services/screen";
import { floatedScreenAgents } from "./screenPageModel";
import type { StoredServer } from "../../services/storage";

/** How often the current computer is asked which agents use a screen, while one might. */
const POLL_MS = 3000;

/**
 * The agents using a screen on the current computer: their Linux displays and the Mac's screen. It reads
 * only while `enabled` (an agent that could be using one is running) and stops asking an endpoint the
 * computer has said it does not have.
 */
export function useAgentScreens(server: StoredServer | null, enabled: boolean) {
  const [displays, setDisplays] = useState<AgentDisplay[]>([]);
  const [mac, setMac] = useState<ScreenAgent[]>([]);
  const [tick, setTick] = useState(0);
  const missing = useRef({ server: "", display: false, mac: false });
  const refresh = useCallback(() => setTick((n) => n + 1), []);

  // A new computer, or nothing that could use a screen: nothing to show until the next read.
  const serverId = server?.id;
  useEffect(() => {
    setDisplays([]);
    setMac([]);
  }, [serverId, enabled]);

  useEffect(() => {
    if (!server || !enabled) return;
    if (missing.current.server !== server.id) missing.current = { server: server.id, display: false, mac: false };
    const controller = new AbortController();
    const read = async () => {
      if (AppState.currentState !== "active") return;
      const gone = missing.current;
      const [shown, status] = await Promise.all([
        gone.display
          ? null
          : listDisplays(server, controller.signal).then(
              (result) => {
                if (!result.available) gone.display = true;
                return result.displays;
              },
              (error) => {
                if (error instanceof DisplayRequestError && (error.status === 404 || error.status === 503)) gone.display = true;
                return null;
              },
            ),
        gone.mac
          ? null
          : screenRequest(server, undefined, controller.signal).then(
              (result) => {
                if (!result.supported) gone.mac = true;
                return result;
              },
              () => null,
            ),
      ]);
      if (controller.signal.aborted) return;
      if (shown) setDisplays(shown);
      if (status) setMac(floatedScreenAgents(status));
    };
    void read();
    const timer = setInterval(() => void read(), POLL_MS);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [server, enabled, tick]);

  return { displays, mac, refresh };
}
