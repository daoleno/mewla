import React, { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import AsyncStorage from "@react-native-async-storage/async-storage";
import { catLabsKey } from "../services/catPark";
import { wsClient } from "../services/websocket";
import { useCurrentServer } from "./currentServer";

interface CatLabsContextValue {
  /** Settings → Labs → Cat for the current computer: on, the Cat tab shows. */
  enabled: boolean;
  /** Whether the switch can be flipped now: a current computer, connected. */
  available: boolean;
  setEnabled(on: boolean): void;
}

const CatLabsContext = createContext<CatLabsContextValue | null>(null);

/**
 * The cat's Labs switch. It is kept on the computer, beside the cat's own
 * state, so the App and the web UI agree; the App only remembers the last
 * answer per computer so the tab does not flicker on launch. It starts off.
 */
export function CatLabsProvider({ children }: { children: ReactNode }) {
  const { currentServerId } = useCurrentServer();
  const serverId = currentServerId ?? "";
  const [state, setState] = useState<{ serverId: string; enabled: boolean; connected: boolean }>({ serverId: "", enabled: false, connected: false });

  useEffect(() => {
    setState({ serverId, enabled: false, connected: false });
    if (!serverId) return;
    let cancelled = false;
    let heard = false;
    const key = catLabsKey(serverId);
    void AsyncStorage.getItem(key).then((raw) => {
      if (!cancelled && !heard && raw) setState((current) => ({ ...current, enabled: raw === "1" }));
    }).catch(() => undefined);

    const remember = (enabled: boolean) => {
      void AsyncStorage.setItem(key, enabled ? "1" : "0").catch(() => undefined);
    };
    const onState = (payload: { serverId?: string; cat?: { enabled?: unknown } }) => {
      if (cancelled || payload.serverId !== serverId || !payload.cat) return;
      heard = true;
      const enabled = payload.cat.enabled === true;
      setState((current) => (current.enabled === enabled && current.connected ? current : { serverId, enabled, connected: true }));
      remember(enabled);
    };
    const ask = () => {
      if (!wsClient.isConnected(serverId)) return;
      setState((current) => (current.connected ? current : { ...current, connected: true }));
      try {
        wsClient.send(serverId, { type: "cat_get" });
      } catch {
        // Not connected after all: the remembered answer stands.
      }
    };
    const onConnected = (payload: { serverId?: string }) => {
      if (payload.serverId === serverId) ask();
    };
    const onDisconnected = (payload: { serverId?: string }) => {
      if (payload.serverId === serverId) setState((current) => (current.connected ? { ...current, connected: false } : current));
    };
    wsClient.on("cat_state", onState);
    wsClient.on("connected", onConnected);
    wsClient.on("disconnected", onDisconnected);
    ask();
    return () => {
      cancelled = true;
      wsClient.off("cat_state", onState);
      wsClient.off("connected", onConnected);
      wsClient.off("disconnected", onDisconnected);
    };
  }, [serverId]);

  const setEnabled = useCallback((on: boolean) => {
    if (!serverId) return;
    try {
      wsClient.send(serverId, { type: "cat_labs", on });
    } catch {
      return; // offline: nothing changes, and the switch says so
    }
    setState((current) => ({ ...current, enabled: on }));
    void AsyncStorage.setItem(catLabsKey(serverId), on ? "1" : "0").catch(() => undefined);
  }, [serverId]);

  const current = state.serverId === serverId ? state : { enabled: false, connected: false };
  const value = useMemo<CatLabsContextValue>(() => ({
    enabled: !!serverId && current.enabled,
    available: !!serverId && current.connected,
    setEnabled,
  }), [current.connected, current.enabled, serverId, setEnabled]);

  return <CatLabsContext.Provider value={value}>{children}</CatLabsContext.Provider>;
}

export function useCatLabs(): CatLabsContextValue {
  const value = useContext(CatLabsContext);
  if (!value) throw new Error("useCatLabs must be used inside CatLabsProvider");
  return value;
}
