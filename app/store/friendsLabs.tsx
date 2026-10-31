import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { AppState, Linking } from "react-native";
import { PET_PACKS, DEFAULT_PET_ID } from "../components/pets/petPacks";
import { findPetPack } from "../components/pets/petModel";
import {
  deleteFriendsAccount,
  disableFriends,
  enableFriends,
  getFriendsStatus,
  pollComputerSignIn,
  setAutoMedia,
  setAutoPost,
  startComputerSignIn,
  type LoginStart,
} from "../services/friendsAccount";
import type { FriendsStatus } from "../services/friendsApi";
import { useThemeContext } from "../theme";
import { useCurrentServer } from "./currentServer";

export type FriendsLabsPhase = "off" | "signing_in" | "working" | "on";

interface FriendsLabsValue {
  phase: FriendsLabsPhase;
  /** What the current computer says. Null until it has answered. */
  status: FriendsStatus | null;
  /** The code to enter while this computer signs in to its Mewla account. */
  signIn: LoginStart | null;
  error: string | null;
  /** There is a current computer to turn Friends on for. */
  available: boolean;
  enabled: boolean;
  draftCount: number;
  unread: number;
  refresh(): Promise<void>;
  /** On: signs the computer in first when it is not, then creates the Friends person. */
  turnOn(): Promise<void>;
  /** Off: stops Friends on the computer and keeps the account, its posts and the drafts. */
  turnOff(): Promise<void>;
  /** Deletes the Friends account, everything it made and the computer's drafts; Friends is off after. Ask first. */
  deleteAccount(): Promise<boolean>;
  setAutoPost(enabled: boolean): Promise<void>;
  setAutoMedia(media: boolean): Promise<void>;
  cancelSignIn(): void;
  openSignInPage(): void;
}

const FriendsLabsContext = createContext<FriendsLabsValue | null>(null);

/**
 * The Friends switch in Labs, and what every Friends screen reads. The
 * current computer is the only truth: it holds the sign-in, the drafts and
 * the link to the person, so a server switch drops what was known and reads
 * the new computer.
 */
export function FriendsLabsProvider({ children }: { children: ReactNode }) {
  const { currentServer } = useCurrentServer();
  const petId = useThemeContext().petId;
  const [status, setStatus] = useState<FriendsStatus | null>(null);
  const [working, setWorking] = useState(false);
  const [signIn, setSignIn] = useState<LoginStart | null>(null);
  const [error, setError] = useState<string | null>(null);
  const serverRef = useRef(currentServer);
  serverRef.current = currentServer;
  const signInToken = useRef(0);

  const refresh = useCallback(async () => {
    const server = serverRef.current;
    if (!server) return;
    try {
      const next = await getFriendsStatus(server);
      if (serverRef.current?.id === server.id) setStatus(next);
    } catch {
      // The old status stays: a computer that does not answer is not "off".
    }
  }, []);

  const serverId = currentServer?.id ?? null;
  useEffect(() => {
    signInToken.current += 1;
    setStatus(null);
    setSignIn(null);
    setError(null);
    setWorking(false);
    if (serverId) void refresh();
  }, [serverId, refresh]);

  useEffect(() => {
    const subscription = AppState.addEventListener("change", (state) => {
      if (state === "active") void refresh();
    });
    return () => subscription.remove();
  }, [refresh]);

  const createPerson = useCallback(
    async (server: NonNullable<typeof currentServer>) => {
      const pack = findPetPack(PET_PACKS, petId, DEFAULT_PET_ID);
      setStatus(await enableFriends(server, pack.name.en, pack.id));
    },
    [petId],
  );

  const turnOn = useCallback(async () => {
    const server = serverRef.current;
    if (!server) {
      setError("Connect a computer first");
      return;
    }
    setError(null);
    setWorking(true);
    const token = ++signInToken.current;
    try {
      const current = await getFriendsStatus(server);
      setStatus(current);
      if (!current.signed_in) {
        const start = await startComputerSignIn(server);
        setSignIn(start);
        const interval = Math.max(2, start.interval_seconds || 5) * 1000;
        for (;;) {
          await new Promise((resolve) => setTimeout(resolve, interval));
          if (signInToken.current !== token) return;
          if (start.expires_at_ms && Date.now() > start.expires_at_ms) throw new Error("The sign-in code expired; try again");
          if (await pollComputerSignIn(server, start.login_id)) break;
        }
        setSignIn(null);
      }
      if (signInToken.current !== token) return;
      await createPerson(server);
    } catch (reason) {
      if (signInToken.current === token) setError(reason instanceof Error ? reason.message : "Friends did not turn on");
    } finally {
      if (signInToken.current === token) {
        setSignIn(null);
        setWorking(false);
      }
    }
  }, [createPerson]);

  const turnOff = useCallback(async () => {
    const server = serverRef.current;
    if (!server) return;
    setError(null);
    // The switch flips at once; it flips back if the computer cannot be told.
    const before = status;
    setStatus((current) => (current ? { ...current, enabled: false, me: undefined, drafts: [] } : current));
    try {
      const next = await disableFriends(server);
      if (serverRef.current?.id === server.id) setStatus(next);
    } catch (reason) {
      if (serverRef.current?.id === server.id) {
        setStatus(before);
        setError(reason instanceof Error ? reason.message : "Friends did not turn off");
      }
    }
  }, [status]);

  const deleteAccount = useCallback(async () => {
    const server = serverRef.current;
    if (!server) return false;
    setError(null);
    try {
      const next = await deleteFriendsAccount(server);
      if (serverRef.current?.id === server.id) setStatus(next);
      return true;
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Friends account was not deleted");
      return false;
    }
  }, []);

  const changeAutoPost = useCallback(async (enabled: boolean) => {
    const server = serverRef.current;
    if (!server) return;
    setError(null);
    try {
      setStatus(await setAutoPost(server, enabled));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not change auto-post");
    }
  }, []);

  const changeAutoMedia = useCallback(async (media: boolean) => {
    const server = serverRef.current;
    if (!server) return;
    setError(null);
    try {
      setStatus(await setAutoMedia(server, media));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not change what auto-post may attach");
    }
  }, []);

  const cancelSignIn = useCallback(() => {
    signInToken.current += 1;
    setSignIn(null);
    setWorking(false);
  }, []);

  const openSignInPage = useCallback(() => {
    const url = signIn?.verification_uri_complete || signIn?.verification_uri;
    if (url) void Linking.openURL(url).catch(() => undefined);
  }, [signIn]);

  const enabled = status?.enabled === true;
  const value = useMemo<FriendsLabsValue>(
    () => ({
      phase: signIn ? "signing_in" : working ? "working" : enabled ? "on" : "off",
      status,
      signIn,
      error,
      available: currentServer !== null,
      enabled,
      draftCount: status?.drafts.length ?? 0,
      unread: status?.me?.unread ?? 0,
      refresh,
      turnOn,
      turnOff,
      deleteAccount,
      setAutoPost: changeAutoPost,
      setAutoMedia: changeAutoMedia,
      cancelSignIn,
      openSignInPage,
    }),
    [signIn, working, enabled, status, error, currentServer, refresh, turnOn, turnOff, deleteAccount, changeAutoPost, changeAutoMedia, cancelSignIn, openSignInPage],
  );
  return <FriendsLabsContext.Provider value={value}>{children}</FriendsLabsContext.Provider>;
}

export function useFriendsLabs(): FriendsLabsValue {
  const value = useContext(FriendsLabsContext);
  if (!value) throw new Error("useFriendsLabs needs FriendsLabsProvider");
  return value;
}
