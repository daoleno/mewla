import { useCallback, useEffect, useRef, useState } from "react";
import { Platform } from "react-native";
import { changeIPhone, getIPhoneStatus, type IPhoneChange, type IPhoneDaemonStatus } from "../services/iphoneApi";
import { useCurrentServer } from "./currentServer";

export interface IPhoneUse {
  /** The current computer's answer; null until read, or when it has no iPhone use (an older Mewla). */
  status: IPhoneDaemonStatus | null;
  error: string | null;
  busy: boolean;
  change(change: IPhoneChange): Promise<void>;
  refresh(): Promise<void>;
}

/**
 * iPhone use on the one current computer, read while a screen shows it: every pollMs (0 reads
 * once). A server switch drops the old computer's answer before the new one's arrives.
 */
export function useIPhoneUse(pollMs = 0): IPhoneUse {
  const { currentServer } = useCurrentServer();
  const [status, setStatus] = useState<IPhoneDaemonStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const serverId = currentServer?.id ?? null;
  const current = useRef(serverId);
  current.current = serverId;

  const refresh = useCallback(async () => {
    if (!currentServer || Platform.OS === "web") return;
    const asked = currentServer.id;
    try {
      const next = await getIPhoneStatus(currentServer);
      if (current.current === asked) {
        setStatus(next);
        setError(null);
      }
    } catch (cause) {
      if (current.current === asked) {
        // An older Mewla has no /iphone: no iPhone use there.
        setStatus(null);
        setError(cause instanceof Error ? cause.message : "Not reachable");
      }
    }
    // The server is the dependency, not its object identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId]);

  useEffect(() => {
    setStatus(null);
    setError(null);
    void refresh();
    if (pollMs <= 0) return;
    const timer = setInterval(() => void refresh(), pollMs);
    return () => clearInterval(timer);
  }, [refresh, pollMs]);

  const change = useCallback(async (next: IPhoneChange) => {
    if (!currentServer) return;
    const asked = currentServer.id;
    setBusy(true);
    try {
      const answer = await changeIPhone(currentServer, next);
      if (current.current === asked) {
        setStatus(answer);
        setError(null);
      }
    } catch (cause) {
      if (current.current === asked) setError(cause instanceof Error ? cause.message : "Not reachable");
    } finally {
      setBusy(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId]);

  return { status, error, busy, change, refresh };
}
