import React, { useCallback, useEffect, useRef, useState } from "react";
import { AppState, StyleSheet, View } from "react-native";
import { ContinuousCorners, Radii, Spacing, useAppTheme } from "../../constants/tokens";
import type { AgentDisplay } from "../../services/display";
import { displayRoute } from "../../services/displayRoute";
import { ScreenConnection } from "../../services/screen";
import type { StoredServer } from "../../services/storage";
import { ActionMenu, AppText, IconButton, InlineNotice } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { LINUX_MODIFIERS, needsSoftKeyboard, RemoteView, type RemoteViewHandle } from "./RemoteView";
import type { ScreenViewerMessage } from "./screenViewerProtocol";

type Phase = "connecting" | "live" | "lost";

export interface AgentDisplayCardProps {
  server: StoredServer;
  display: AgentDisplay;
  /** The Worker's name, when it has one. */
  agentName?: string;
  /** Stop this agent's use of its display. */
  onStop(worker: string): Promise<void>;
  /** Let a stopped agent use its display again. */
  onResume(worker: string): Promise<void>;
}

/**
 * One agent's private display on a Linux computer. The live view is the input
 * surface: what the person does reaches the display while the agent keeps
 * going, and the agent is told how many clicks and keys and when (never what
 * was typed) and decides for itself. The menu holds the two hard switches:
 * Hold (the agent waits until released or the view closes) and Stop.
 */
export function AgentDisplayCard({ server, display, agentName, onStop, onResume }: AgentDisplayCardProps) {
  const { colors } = useAppTheme();
  const view = useRef<RemoteViewHandle>(null);
  const connection = useRef<ScreenConnection | null>(null);
  const epoch = useRef(0);
  const serverRef = useRef(server);
  serverRef.current = server;
  const [phase, setPhase] = useState<Phase>("connecting");
  const [holding, setHolding] = useState(false);
  const [typing, setTyping] = useState(false);
  const [menu, setMenu] = useState(false);
  const [busy, setBusy] = useState(false);
  const [reason, setReason] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const worker = display.id;

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
    }, false, undefined, displayRoute(worker));
    connection.current = socket;
    try { await socket.connect(); } catch (error) {
      if (!active()) return;
      setReason(error instanceof Error ? error.message : String(error));
      setPhase("lost");
    }
  }, [disconnect, worker]);

  useEffect(() => {
    void connect();
    // A phone that leaves the app gives its viewers up; coming back reconnects them.
    const subscription = AppState.addEventListener("change", (state) => {
      if (state !== "active") { disconnect(); setHolding(false); setPhase("lost"); }
      else void connect();
    });
    return () => { subscription.remove(); disconnect(); };
  }, [server.id, worker, attempt, connect, disconnect]);

  const onEvent = useCallback((message: ScreenViewerMessage) => {
    if (message.type === "connected") setPhase("live");
    else if (message.type === "closed" && connection.current) {
      connection.current.close();
      connection.current = null;
      setPhase("lost");
    }
  }, []);
  const onSend = useCallback((data: string) => connection.current?.send(data), []);

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true);
    try { await action(); } finally { setBusy(false); }
  };
  const hold = (on: boolean) => {
    connection.current?.control({ type: "hold", on });
    setHolding(on);
  };

  const live = phase === "live";
  const elsewhere = !holding && display.control === "person";
  const title = agentName || display.id;
  const subtitle = holding ? "You hold the agent" : elsewhere ? "Held on another device" : display.stopped ? "Stopped" : null;
  const items: ActionMenuItem[] = [
    ...(live ? [holding
      ? { key: "release", icon: "play", label: "Let the agent go on", onPress: () => hold(false) } as const
      : { key: "hold", icon: "hand", label: "Hold the agent", onPress: () => hold(true) } as const] : []),
    ...(live ? [{ key: "fit", icon: "expand", label: "Fit", onPress: () => view.current?.fit() } as const] : []),
    ...(live && !needsSoftKeyboard() ? [{ key: "full", icon: "expand", label: "Full size", onPress: () => view.current?.fullSize() } as const] : []),
    display.stopped
      ? { key: "resume", icon: "play", label: "Resume", disabled: busy, onPress: () => void run(() => onResume(worker)) } as const
      : { key: "stop", icon: "stop", label: "Stop", destructive: true, disabled: busy || elsewhere, accessibilityLabel: `Stop ${title}`, onPress: () => void run(() => onStop(worker)) } as const,
    ...(phase === "lost" ? [{ key: "reconnect", icon: "refresh", label: "Reconnect", onPress: () => setAttempt((n) => n + 1) } as const] : []),
  ];
  const dot = holding ? colors.seal : display.stopped || elsewhere ? colors.textTertiary : colors.accent;

  return (
    <View style={[styles.card, { backgroundColor: colors.bgSurface, borderColor: colors.borderSubtle }]}>
      <View style={styles.header}>
        <View style={[styles.dot, { backgroundColor: dot }]} />
        <View style={styles.headerText}>
          <AppText variant="label" numberOfLines={1} style={{ color: colors.textPrimary }}>{title}</AppText>
          {subtitle ? <AppText variant="caption" numberOfLines={1} style={{ color: colors.textSecondary }}>{subtitle}</AppText> : null}
        </View>
        {live && needsSoftKeyboard() ? (
          <IconButton icon="keyboard" size={40} tone={typing ? "tinted" : "default"} accessibilityLabel={typing ? "Hide keyboard" : "Show keyboard"}
            onPress={() => view.current?.toggleKeyboard()} />
        ) : null}
        <IconButton icon="more-vertical" size={40} accessibilityLabel={`Options for ${title}`} onPress={() => setMenu(true)} />
      </View>
      {phase === "lost" ? (
        <View style={styles.notice}>
          <InlineNotice tone="warning" title="Lost connection to the display" detail={reason || "It may have been closed."}
            action={{ label: "Reconnect", onPress: () => setAttempt((n) => n + 1) }} />
        </View>
      ) : phase === "connecting" ? <View style={styles.notice}><InlineNotice busy title="Connecting…" /></View> : null}
      <RemoteView ref={view} backgroundColor={colors.bgElevated} accessibilityLabel={`Display of ${title}`} align="top"
        modifiers={LINUX_MODIFIERS} viewStyle={styles.view} onSend={onSend} onEvent={onEvent} onKeyboardChange={setTyping} />
      <ActionMenu visible={menu} title={title} items={items} onClose={() => setMenu(false)} />
    </View>
  );
}

const styles = StyleSheet.create({
  card: { borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.md, overflow: "hidden", ...ContinuousCorners },
  header: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, paddingLeft: Spacing.md, paddingRight: Spacing.xs, paddingVertical: Spacing.xs },
  headerText: { flex: 1 },
  dot: { width: 8, height: 8, borderRadius: 4 },
  notice: { paddingHorizontal: Spacing.sm, paddingBottom: Spacing.sm },
  view: { width: "100%", aspectRatio: 1280 / 800 },
});
