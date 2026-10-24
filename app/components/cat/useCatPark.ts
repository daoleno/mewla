import { useCallback, useEffect, useRef, useState } from "react";
import AsyncStorage from "@react-native-async-storage/async-storage";
import { catCacheKey, catSocketMessage, parseCatView, type CatBridgeMessage, type CatView } from "../../services/catPark";
import { wsClient } from "../../services/websocket";
import { useCurrentServer } from "../../store/currentServer";

/**
 * The cat's state for the one current computer: the daemon's cat_state over the
 * app's own WebSocket, cached per computer so the house opens offline with what
 * it last knew. A server switch drops the old computer's state first.
 */
export function useCatPark() {
  const { currentServer } = useCurrentServer();
  const serverId = currentServer?.id ?? "";
  const [view, setView] = useState<{ serverId: string; value: CatView } | null>(null);
  const serverRef = useRef(serverId);
  serverRef.current = serverId;

  useEffect(() => {
    setView(null);
    if (!serverId) return;
    let cancelled = false;
    const key = catCacheKey(serverId);
    void AsyncStorage.getItem(key).then((raw) => {
      if (cancelled || !raw) return;
      try {
        const cached = parseCatView(JSON.parse(raw));
        if (cached) setView((current) => current ?? { serverId, value: cached });
      } catch {
        // A damaged cache is just no cache.
      }
    }).catch(() => undefined);

    const apply = (value: CatView) => {
      if (cancelled) return;
      setView({ serverId, value });
      void AsyncStorage.setItem(key, JSON.stringify(value)).catch(() => undefined);
    };
    const onState = (payload: { serverId?: string; cat?: unknown }) => {
      if (payload.serverId !== serverId) return;
      const next = parseCatView(payload.cat);
      if (next) apply(next);
    };
    const ask = () => {
      if (!wsClient.isConnected(serverId)) return;
      try {
        wsClient.send(serverId, { type: "cat_get" });
      } catch {
        // Not connected after all: the cached state stands.
      }
    };
    const onConnected = (payload: { serverId?: string }) => {
      if (payload.serverId === serverId) ask();
    };
    wsClient.on("cat_state", onState);
    wsClient.on("connected", onConnected);
    ask();
    return () => {
      cancelled = true;
      wsClient.off("cat_state", onState);
      wsClient.off("connected", onConnected);
    };
  }, [serverId]);

  /** Sends a page action to the daemon; actions the App owns are ignored here. */
  const act = useCallback((message: CatBridgeMessage) => {
    const wire = catSocketMessage(message);
    const id = serverRef.current;
    if (!wire || !id) return;
    try {
      wsClient.send(id, wire);
    } catch {
      // Offline: the page already shows the change; the daemon's state wins on reconnect.
    }
  }, []);

  return {
    server: currentServer,
    view: view && view.serverId === serverId ? view.value : null,
    act,
  };
}
