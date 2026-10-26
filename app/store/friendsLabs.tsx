import AsyncStorage from "@react-native-async-storage/async-storage";
import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  getComputerFriends,
  linkComputer,
  signInToFriends,
  unlinkComputer,
  type ComputerFriends,
} from "../services/friendsAccount";
import { useCurrentServer } from "./currentServer";

const ENABLED_KEY = "mewla.friends.enabled";

export type FriendsLabsPhase = "off" | "working" | "on";

interface FriendsLabsValue {
  phase: FriendsLabsPhase;
  /** The handle of the signed-in account. */
  handle: string | null;
  /** Whether the current computer's Brain is this account's agent. Unknown until read. */
  linked: boolean | null;
  error: string | null;
  /** The current computer; Friends needs one to link. */
  available: boolean;
  setEnabled(enabled: boolean): Promise<void>;
  /** Makes the current computer's Brain the agent, replacing the one before. */
  useThisComputer(): Promise<void>;
}

const FriendsLabsContext = createContext<FriendsLabsValue | null>(null);

/**
 * The Friends switch in Labs. Off by default. Turning it on signs in with
 * GitHub and links the current computer; the current computer is the only one
 * that is ever the agent, so a server switch asks to rebind rather than
 * keeping the old link.
 */
export function FriendsLabsProvider({ children }: { children: ReactNode }) {
  const { currentServer } = useCurrentServer();
  const [enabled, setEnabledState] = useState(false);
  const [working, setWorking] = useState(false);
  const [handle, setHandle] = useState<string | null>(null);
  const [computer, setComputer] = useState<ComputerFriends | null>(null);
  const [error, setError] = useState<string | null>(null);
  const serverRef = useRef(currentServer);
  serverRef.current = currentServer;

  useEffect(() => {
    let cancelled = false;
    AsyncStorage.getItem(ENABLED_KEY)
      .then((value) => {
        if (!cancelled) setEnabledState(value === "1");
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  // Rebind on a server switch: what this computer says is the only truth.
  const serverId = currentServer?.id ?? null;
  useEffect(() => {
    setComputer(null);
    if (!enabled || !currentServer) return;
    let cancelled = false;
    getComputerFriends(currentServer)
      .then((status) => {
        if (cancelled) return;
        setComputer(status);
        if (status.handle) setHandle(status.handle);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, serverId]);

  const setEnabled = useCallback(async (next: boolean) => {
    const server = serverRef.current;
    setError(null);
    if (!next) {
      setEnabledState(false);
      await AsyncStorage.removeItem(ENABLED_KEY).catch(() => undefined);
      setComputer(null);
      if (server) await unlinkComputer(server).catch(() => undefined);
      return;
    }
    if (!server) {
      setError("Connect a computer first");
      return;
    }
    setWorking(true);
    try {
      const me = await signInToFriends();
      const status = await linkComputer(server);
      setHandle(me.person.handle);
      setComputer(status);
      setEnabledState(true);
      await AsyncStorage.setItem(ENABLED_KEY, "1").catch(() => undefined);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Friends did not turn on");
    } finally {
      setWorking(false);
    }
  }, []);

  const useThisComputer = useCallback(async () => {
    const server = serverRef.current;
    if (!server) return;
    setError(null);
    setWorking(true);
    try {
      setComputer(await linkComputer(server));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not link this computer");
    } finally {
      setWorking(false);
    }
  }, []);

  const value = useMemo<FriendsLabsValue>(
    () => ({
      phase: working ? "working" : enabled ? "on" : "off",
      handle,
      linked: computer ? computer.linked : null,
      error,
      available: currentServer !== null,
      setEnabled,
      useThisComputer,
    }),
    [working, enabled, handle, computer, error, currentServer, setEnabled, useThisComputer],
  );
  return <FriendsLabsContext.Provider value={value}>{children}</FriendsLabsContext.Provider>;
}

export function useFriendsLabs(): FriendsLabsValue {
  const value = useContext(FriendsLabsContext);
  if (!value) throw new Error("useFriendsLabs needs FriendsLabsProvider");
  return value;
}
