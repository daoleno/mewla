import React, { useCallback, useEffect, useRef, useState } from "react";
import { AppState, StyleSheet, TextInput, View } from "react-native";
import { ContinuousCorners, Radii, Spacing, TouchTarget, useAppTheme } from "../../constants/tokens";
import type { AgentDisplay } from "../../services/display";
import { displayRoute } from "../../services/displayRoute";
import { ScreenConnection } from "../../services/screen";
import type { StoredServer } from "../../services/storage";
import { AppText, Button, IconButton, InlineNotice } from "../ui";
import { ScreenViewerHost } from "./ScreenViewerHost";
import { parseScreenViewerMessage, type ScreenHostMessage, type ScreenKey, type ScreenViewerHostHandle } from "./screenViewerProtocol";

type Phase = "connecting" | "live" | "lost";

export interface AgentDisplayCardProps {
  server: StoredServer;
  display: AgentDisplay;
  /** The Worker's name, when it has one. */
  agentName?: string;
  /** Stop this agent's use of its display. */
  onStop(worker: string): Promise<void>;
}

/**
 * One agent's private display on a Linux computer, with its own live view.
 * Showing it is Watch: the person's taps and typing reach the display without
 * taking it from the agent, which is told how many and when (never what was
 * typed) and decides for itself. Take over holds the display for the person
 * until they hand it back; Stop ends the agent's use.
 */
export function AgentDisplayCard({ server, display, agentName, onStop }: AgentDisplayCardProps) {
  const { colors } = useAppTheme();
  const host = useRef<ScreenViewerHostHandle>(null);
  const connection = useRef<ScreenConnection | null>(null);
  const viewerReady = useRef(false);
  const outbox = useRef<ScreenHostMessage[]>([]);
  const chunks = useRef<string[]>([]);
  const flushQueued = useRef(false);
  const epoch = useRef(0);
  const serverRef = useRef(server);
  serverRef.current = server;
  const [phase, setPhase] = useState<Phase>("connecting");
  const [mine, setMine] = useState(false);
  const [typing, setTyping] = useState(false);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [reason, setReason] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const worker = display.id;

  const flush = useCallback(() => {
    flushQueued.current = false;
    if (!viewerReady.current) return;
    const batch = outbox.current;
    outbox.current = [];
    if (chunks.current.length) {
      batch.push({ type: "rfb", data: chunks.current, at: Date.now() });
      chunks.current = [];
    }
    if (batch.length) host.current?.post(batch);
  }, []);
  const schedule = useCallback(() => {
    if (flushQueued.current) return;
    flushQueued.current = true;
    requestAnimationFrame(flush);
  }, [flush]);
  const toViewer = useCallback((message: ScreenHostMessage) => {
    if (chunks.current.length) {
      outbox.current.push({ type: "rfb", data: chunks.current, at: Date.now() });
      chunks.current = [];
    }
    outbox.current.push(message);
    schedule();
  }, [schedule]);

  const disconnect = useCallback(() => {
    epoch.current++;
    connection.current?.close();
    connection.current = null;
    chunks.current = [];
    outbox.current = [];
    if (viewerReady.current) host.current?.post([{ type: "close" }]);
  }, []);

  const connect = useCallback(async (takeOver: boolean) => {
    disconnect();
    const token = epoch.current;
    const active = () => token === epoch.current;
    setPhase("connecting");
    setReason(null);
    const socket = new ScreenConnection(serverRef.current, {
      open() { if (active()) toViewer({ type: "open" }); },
      data(chunk) {
        if (!active()) return;
        chunks.current.push(chunk);
        schedule();
      },
      ready() {},
      rtt() {},
      closed(why) {
        if (!active()) return;
        toViewer({ type: "close" });
        setReason(why || null);
        setMine(false);
        setPhase("lost");
      },
    }, false, undefined, displayRoute(worker, takeOver));
    connection.current = socket;
    try { await socket.connect(); } catch (error) {
      if (!active()) return;
      setReason(error instanceof Error ? error.message : String(error));
      setPhase("lost");
    }
  }, [disconnect, schedule, toViewer, worker]);

  useEffect(() => {
    setMine(false);
    void connect(false);
    // A phone that leaves the app gives its viewers up; coming back reconnects them.
    const subscription = AppState.addEventListener("change", (state) => {
      if (state !== "active") { disconnect(); setMine(false); setPhase("lost"); }
      else void connect(false);
    });
    return () => { subscription.remove(); disconnect(); };
  }, [server.id, worker, attempt, connect, disconnect]);

  const onViewerMessage = useCallback((raw: string) => {
    const message = parseScreenViewerMessage(raw);
    if (!message) return;
    switch (message.type) {
      case "ready":
        viewerReady.current = true;
        host.current?.post([{ type: "mode", mode: "touch" }, { type: "align", align: "top" }]);
        flush();
        break;
      case "send": connection.current?.send(message.data); break;
      case "closed":
        if (connection.current) {
          connection.current.close();
          connection.current = null;
          setPhase("lost");
        }
        break;
      case "connected": setPhase("live"); break;
    }
  }, [flush]);

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true);
    try { await action(); } finally { setBusy(false); }
  };
  const takeOver = () => run(async () => { setMine(true); await connect(true); });
  const handBack = () => run(async () => { setMine(false); await connect(false); });
  const key = (name: ScreenKey) => toViewer({ type: "key", key: name, mods: [] });

  const holder = mine ? "you" : display.control === "person" ? "device" : "agent";
  const title = agentName || display.id;
  const subtitle = holder === "you" ? "Desktop · You are in control" : holder === "device" ? "Desktop · In use on another device" : display.stopped ? "Desktop · Stopped" : "Desktop";
  const live = phase === "live";
  const inputStyle = [styles.input, { color: colors.textPrimary, backgroundColor: colors.inputBackground, borderColor: colors.border }];

  return (
    <View style={[styles.card, { backgroundColor: colors.bgSurface, borderColor: colors.borderSubtle }]}>
      <View style={styles.header}>
        <View style={[styles.dot, { backgroundColor: holder === "agent" && !display.stopped ? colors.accent : holder === "you" ? colors.seal : colors.textTertiary }]} />
        <View style={styles.headerText}>
          <AppText variant="label" numberOfLines={1} style={{ color: colors.textPrimary }}>{title}</AppText>
          <AppText variant="caption" numberOfLines={1} style={{ color: colors.textSecondary }}>{subtitle}</AppText>
        </View>
      </View>
      {phase === "lost" ? (
        <View style={styles.notice}>
          <InlineNotice tone="warning" title="Lost connection to the display" detail={reason || "It may have been closed."}
            action={{ label: "Reconnect", onPress: () => setAttempt((n) => n + 1) }} />
        </View>
      ) : phase === "connecting" ? <View style={styles.notice}><InlineNotice busy title="Connecting…" /></View> : null}
      <View style={[styles.view, { backgroundColor: colors.bgElevated }]}>
        <ScreenViewerHost ref={host} onMessage={onViewerMessage} backgroundColor={colors.bgElevated} accessibilityLabel={`Display of ${title}`} />
      </View>
      {live ? (
        <View style={styles.controls}>
          <IconButton icon="keypad" size={40} tone={typing ? "tinted" : "default"} accessibilityLabel={typing ? "Hide typing tools" : "Show typing tools"}
            accessibilityState={{ expanded: typing }} onPress={() => setTyping(!typing)} />
          <View style={styles.spacer} />
          {holder === "you" ? (
            <Button label="Hand back" variant="tinted" size="sm" disabled={busy} onPress={() => void handBack()} />
          ) : (
            <>
              <Button label="Take over" variant="tinted" size="sm" disabled={busy} onPress={() => void takeOver()} />
              {holder === "agent" && !display.stopped ? (
                <Button label="Stop" variant="outlined" size="sm" accessibilityLabel={`Stop ${title}`} disabled={busy}
                  onPress={() => void run(() => onStop(worker))} />
              ) : null}
            </>
          )}
        </View>
      ) : null}
      {live && typing ? (
        <View style={styles.tray}>
          <View style={styles.bar}>
            <TextInput accessibilityLabel="Text to type into the focused field" secureTextEntry autoCorrect={false} autoCapitalize="none"
              value={text} onChangeText={setText} placeholder="Type into the focused field" placeholderTextColor={colors.textTertiary} style={inputStyle} />
            <Button label="Send" variant="tinted" disabled={!text} onPress={() => { const value = text; setText(""); toViewer({ type: "text", text: value, mods: [] }); }} />
          </View>
          <View style={styles.keys}>
            {([["Tab", "Tab"], ["Return", "Enter"], ["BackSpace", "⌫"], ["Escape", "Esc"]] as const).map(([name, label]) => (
              <Button key={name} label={label} accessibilityLabel={name === "BackSpace" ? "Backspace" : `${label} key`} variant="outlined" style={styles.key}
                onPress={() => key(name)} />
            ))}
          </View>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  card: { borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.md, overflow: "hidden", ...ContinuousCorners },
  header: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, paddingLeft: Spacing.md, paddingRight: Spacing.md, paddingVertical: Spacing.sm },
  headerText: { flex: 1 },
  dot: { width: 8, height: 8, borderRadius: 4 },
  notice: { paddingHorizontal: Spacing.sm, paddingBottom: Spacing.sm },
  view: { width: "100%", aspectRatio: 1280 / 800 },
  controls: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, paddingHorizontal: Spacing.sm, paddingVertical: Spacing.xs },
  spacer: { flex: 1 },
  bar: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, paddingHorizontal: Spacing.sm, paddingVertical: Spacing.xs },
  tray: { paddingBottom: Spacing.sm },
  keys: { flexDirection: "row", gap: Spacing.sm, paddingHorizontal: Spacing.sm },
  key: { flex: 1 },
  input: { minHeight: TouchTarget, flex: 1, paddingHorizontal: Spacing.md, borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.sm, ...ContinuousCorners },
});
