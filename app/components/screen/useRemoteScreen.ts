import { useCallback, useEffect, useRef, useState } from "react";
import { AppState } from "react-native";
import { ScreenConnection } from "../../services/screen";
import type { StoredServer } from "../../services/storage";
import type { ScreenTarget } from "./screenFloatModel";
import type { RemoteViewHandle } from "./RemoteView";
import type { ScreenViewerMessage } from "./screenViewerProtocol";

export type RemotePhase = "connecting" | "live" | "lost";

/**
 * One live socket to an agent's screen, for a RemoteView: connect, give up when the App leaves the
 * foreground and come back with it, and the Hold switch that rides on the socket. The card in Screen
 * and the floating window use it alike.
 */
export function useRemoteScreen(server: StoredServer, target: ScreenTarget | null, key: string) {
  const view = useRef<RemoteViewHandle>(null);
  const connection = useRef<ScreenConnection | null>(null);
  const epoch = useRef(0);
  const serverRef = useRef(server);
  serverRef.current = server;
  const targetRef = useRef(target);
  targetRef.current = target;
  const [phase, setPhase] = useState<RemotePhase>("connecting");
  const [holding, setHolding] = useState(false);
  const [typing, setTyping] = useState(false);
  const [reason, setReason] = useState<string | null>(null);
  const [size, setSize] = useState<{ width: number; height: number } | null>(null);
  const [attempt, setAttempt] = useState(0);

  const disconnect = useCallback(() => {
    epoch.current++;
    connection.current?.close();
    connection.current = null;
    view.current?.close();
  }, []);

  const connect = useCallback(async () => {
    disconnect();
    const token = epoch.current;
    const active = () => token === epoch.current;
    const next = targetRef.current;
    if (!next) { setPhase("lost"); return; }
    setPhase("connecting");
    setReason(null);
    setHolding(false);
    const socket = new ScreenConnection(serverRef.current, {
      open() { if (active()) view.current?.open(); },
      data(chunk) { if (active()) view.current?.feed(chunk); },
      ready() {},
      rtt() {},
      closed(why) {
        if (!active()) return;
        view.current?.close();
        setReason(why || null);
        setHolding(false);
        setPhase("lost");
      },
    }, next.watch, next.worker, next.route);
    connection.current = socket;
    try { await socket.connect(); } catch (error) {
      if (!active()) return;
      setReason(error instanceof Error ? error.message : String(error));
      setPhase("lost");
    }
  }, [disconnect]);

  useEffect(() => {
    void connect();
    // A phone that leaves the app gives its viewers up; coming back reconnects them.
    const subscription = AppState.addEventListener("change", (state) => {
      if (state !== "active") { disconnect(); setHolding(false); setPhase("lost"); }
      else void connect();
    });
    return () => { subscription.remove(); disconnect(); };
  }, [server.id, key, attempt, connect, disconnect]);

  const onEvent = useCallback((message: ScreenViewerMessage) => {
    if (message.type === "connected") {
      setSize(message.width > 0 && message.height > 0 ? { width: message.width, height: message.height } : null);
      setPhase("live");
    } else if (message.type === "closed" && connection.current) {
      connection.current.close();
      connection.current = null;
      setPhase("lost");
    }
  }, []);
  const onSend = useCallback((data: string) => connection.current?.send(data), []);
  const hold = useCallback((on: boolean) => {
    connection.current?.control({ type: "hold", on });
    setHolding(on);
  }, []);
  const reconnect = useCallback(() => setAttempt((n) => n + 1), []);

  return { view, phase, holding, typing, setTyping, reason, size, hold, reconnect, onEvent, onSend };
}
