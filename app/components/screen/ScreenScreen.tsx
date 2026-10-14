import React, { useCallback, useEffect, useRef, useState } from "react";
import {
  AppState,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  useWindowDimensions,
  View,
} from "react-native";
import { Stack, useRouter } from "expo-router";
import * as Haptics from "expo-haptics";
import * as ScreenOrientation from "expo-screen-orientation";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ContinuousCorners, Radii, Spacing, TouchTarget, useAppTheme } from "../../constants/tokens";
import { useCurrentServer } from "../../store/currentServer";
import { ScreenConnection, screenRequest, type ScreenStatus } from "../../services/screen";
import { ActionMenu, Button, EmptyState, IconButton, InlineNotice } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { AppText } from "../ui/AppText";
import { Icon } from "../icons/Icon";
import { ScreenViewerHost } from "./ScreenViewerHost";
import {
  activeModifiers,
  connectionQuality,
  formatStats,
  KEYBOARD_SENTINEL,
  keyboardEdit,
  NO_MODIFIERS,
  powerSummary,
  powerWarning,
  spendModifiers,
  tapModifier,
  type ModifierState,
} from "./screenPageModel";
import {
  parseScreenViewerMessage,
  type ScreenHostMessage,
  type ScreenKey,
  type ScreenModifier,
  type ScreenPointerMode,
  type ScreenViewerHostHandle,
} from "./screenViewerProtocol";

type Phase = "checking" | "setup" | "connecting" | "live" | "lost";

const MODIFIERS: { key: ScreenModifier; label: string; name: string }[] = [
  { key: "Command", label: "⌘", name: "Command" },
  { key: "Control", label: "⌃", name: "Control" },
  { key: "Option", label: "⌥", name: "Option" },
  { key: "Shift", label: "⇧", name: "Shift" },
];
const KEYS: { key: ScreenKey; label: string; name: string }[] = [
  { key: "Escape", label: "esc", name: "Escape" },
  { key: "Tab", label: "tab", name: "Tab" },
  { key: "Left", label: "←", name: "Left arrow" },
  { key: "Up", label: "↑", name: "Up arrow" },
  { key: "Down", label: "↓", name: "Down arrow" },
  { key: "Right", label: "→", name: "Right arrow" },
  { key: "BackSpace", label: "⌫", name: "Delete backward" },
  { key: "Return", label: "↩", name: "Return" },
];
const SHORTCUTS: { label: string; name: string; text?: string; key?: ScreenKey; mods: ScreenModifier[] }[] = [
  { label: "⌘ Space", name: "Spotlight", text: " ", mods: ["Command"] },
  { label: "⌘ Tab", name: "Switch app", key: "Tab", mods: ["Command"] },
  { label: "⌘C", name: "Copy", text: "c", mods: ["Command"] },
  { label: "⌘V", name: "Paste", text: "v", mods: ["Command"] },
  { label: "⌘Z", name: "Undo", text: "z", mods: ["Command"] },
  { label: "⌘W", name: "Close window", text: "w", mods: ["Command"] },
  { label: "⌘Q", name: "Quit app", text: "q", mods: ["Command"] },
];

/**
 * The current server's own screen, through its Screen Sharing. Leaving,
 * backgrounding or switching servers closes the viewer; nothing reconnects
 * on its own except when the app returns to the foreground.
 */
export function ScreenScreen() {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { width, height } = useWindowDimensions();
  const landscape = width > height;
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { colors } = useAppTheme();
  const serverName = currentServer?.name || "this computer";
  const [phase, setPhase] = useState<Phase>("checking");
  const [status, setStatus] = useState<ScreenStatus | null>(null);
  const [reason, setReason] = useState<string | null>(null);
  const [rtt, setRtt] = useState<number | null>(null);
  const [stats, setStats] = useState<{ fps: number; kbps: number; inputMs: number | null; width: number; height: number } | null>(null);
  const [showStats, setShowStats] = useState(true);
  const [mode, setMode] = useState<ScreenPointerMode>("touch");
  const [mods, setMods] = useState<ModifierState>(NO_MODIFIERS);
  const [typing, setTyping] = useState(false);
  const [menu, setMenu] = useState(false);
  const [busy, setBusy] = useState(false);
  const host = useRef<ScreenViewerHostHandle>(null);
  const input = useRef<TextInput>(null);
  const connection = useRef<ScreenConnection | null>(null);
  const viewerReady = useRef(false);
  const outbox = useRef<ScreenHostMessage[]>([]);
  const chunks = useRef<string[]>([]);
  const flushQueued = useRef(false);
  const epoch = useRef(0);
  const modsRef = useRef(mods);
  modsRef.current = mods;
  const modeRef = useRef(mode);
  modeRef.current = mode;

  // RFB bytes go to the viewer once per frame, so a burst of small socket
  // messages costs one bridge crossing.
  const flush = useCallback(() => {
    flushQueued.current = false;
    if (!viewerReady.current) return;
    const batch = outbox.current;
    outbox.current = [];
    if (chunks.current.length) {
      batch.push({ type: "rfb", data: chunks.current });
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
    // Bytes already queued belong before any later control message.
    if (chunks.current.length) {
      outbox.current.push({ type: "rfb", data: chunks.current });
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
    setRtt(null);
    setStats(null);
  }, []);

  const connect = useCallback(async () => {
    if (!currentServer) return;
    const server = currentServer;
    disconnect();
    const token = epoch.current;
    const active = () => token === epoch.current && isCurrentServer(server.id);
    setPhase("checking");
    setReason(null);
    let next: ScreenStatus;
    try {
      next = await screenRequest(server);
    } catch (error) {
      if (!active()) return;
      setStatus(null);
      setReason(error instanceof Error ? error.message : String(error));
      setPhase("lost");
      return;
    }
    if (!active()) return;
    setStatus(next);
    // Unsupported, off at the computer, not yet allowed, or missing
    // permissions: the setup view explains the one next step.
    if (!next.ready) {
      setReason(next.reason || null);
      setPhase("setup");
      return;
    }
    setPhase("connecting");
    const socket = new ScreenConnection(server, {
      open() { if (active()) toViewer({ type: "open" }); },
      data(chunk) {
        if (!active()) return;
        chunks.current.push(chunk);
        schedule();
      },
      ready() {},
      rtt(ms) { if (active()) setRtt(ms); },
      closed(why) {
        if (!active()) return;
        toViewer({ type: "close" });
        setReason(why || "The connection to the screen ended.");
        setPhase("lost");
      },
    });
    connection.current = socket;
    try { await socket.connect(); } catch (error) {
      if (!active()) return;
      setReason(error instanceof Error ? error.message : String(error));
      setPhase("lost");
    }
  }, [currentServer, disconnect, isCurrentServer, schedule, toViewer]);

  useEffect(() => {
    setStatus(null);
    void connect();
    const subscription = AppState.addEventListener("change", (state) => {
      if (state === "active") void connect();
      else disconnect();
    });
    return () => { subscription.remove(); disconnect(); };
  }, [currentServer?.id, connect, disconnect]);

  // Only this page may rotate; the rest of the app stays portrait.
  useEffect(() => {
    if (Platform.OS === "web") return;
    void ScreenOrientation.unlockAsync().catch(() => {});
    return () => { void ScreenOrientation.lockAsync(ScreenOrientation.OrientationLock.PORTRAIT_UP).catch(() => {}); };
  }, []);

  // Power state changes slowly; refresh it while the viewer is open.
  useEffect(() => {
    if (phase !== "live" || !currentServer) return;
    const server = currentServer;
    const timer = setInterval(() => {
      void screenRequest(server).then((next) => { if (isCurrentServer(server.id)) setStatus(next); }).catch(() => {});
    }, 30000);
    return () => clearInterval(timer);
  }, [currentServer, isCurrentServer, phase]);

  const onViewerMessage = useCallback((raw: string) => {
    const message = parseScreenViewerMessage(raw);
    if (!message) return;
    switch (message.type) {
      case "ready":
        viewerReady.current = true;
        host.current?.post([{ type: "mode", mode: modeRef.current }]);
        flush();
        break;
      case "send": connection.current?.send(message.data); break;
      case "closed":
        // The RFB client gave up (protocol error); end the socket too.
        if (connection.current) {
          connection.current.close();
          connection.current = null;
          setReason("The screen viewer stopped. Reconnect to continue.");
          setPhase("lost");
        }
        break;
      case "connected": setPhase("live"); break;
      case "stats": setStats(message); break;
      case "haptic": if (Platform.OS !== "web") void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium).catch(() => {}); break;
    }
  }, [flush]);

  const sendKey = (key: ScreenKey, extra: ScreenModifier[] = []) => {
    toViewer({ type: "key", key, mods: [...new Set([...activeModifiers(modsRef.current), ...extra])] });
    setMods(spendModifiers(modsRef.current));
  };
  const sendText = (text: string, extra: ScreenModifier[] = []) => {
    toViewer({ type: "text", text, mods: [...new Set([...activeModifiers(modsRef.current), ...extra])] });
    setMods(spendModifiers(modsRef.current));
  };
  const onKeyboardText = (next: string) => {
    const edit = keyboardEdit(next);
    if (edit.kind === "text") sendText(edit.text);
    else if (edit.kind === "backspace") for (let i = 0; i < edit.count; i++) sendKey("BackSpace");
  };
  const toggleKeyboard = () => {
    if (typing) { input.current?.blur(); setTyping(false); }
    else { setTyping(true); requestAnimationFrame(() => input.current?.focus()); }
  };
  const setKeepAwake = async (keep: boolean) => {
    if (!currentServer) return;
    const server = currentServer;
    setBusy(true);
    try {
      const next = await screenRequest(server, { keep_awake: keep });
      if (isCurrentServer(server.id)) setStatus(next);
    } catch (error) {
      if (isCurrentServer(server.id)) setReason(error instanceof Error ? error.message : String(error));
    } finally { setBusy(false); }
  };
  // Turning Screen on waits for someone at the computer to click Allow; the
  // operating system then asks for its own permissions there.
  const enable = async () => {
    if (!currentServer) return;
    const server = currentServer;
    setBusy(true);
    setReason(null);
    try {
      const next = await screenRequest(server, { action: "enable" });
      if (!isCurrentServer(server.id)) return;
      setStatus(next);
      if (next.ready) void connect();
    } catch (error) {
      if (isCurrentServer(server.id)) setReason(error instanceof Error ? error.message : String(error));
    } finally { setBusy(false); }
  };
  const openSettings = async (permission: string) => {
    if (!currentServer) return;
    try { await screenRequest(currentServer, { action: "open_settings", permission }); } catch (error) {
      setReason(error instanceof Error ? error.message : String(error));
    }
  };
  // Restarting drops this connection; keep checking until Mewla answers.
  const restartMewla = async () => {
    if (!currentServer) return;
    const server = currentServer;
    setBusy(true);
    setReason(null);
    try {
      await screenRequest(server, { action: "restart" });
      for (let attempt = 0; attempt < 30 && isCurrentServer(server.id); attempt++) {
        await new Promise((resolve) => setTimeout(resolve, 2000));
        try {
          const next = await screenRequest(server);
          if (!isCurrentServer(server.id)) return;
          setStatus(next);
          if (!next.permissions?.some((p) => p.restart_required)) break;
        } catch { /* still restarting */ }
      }
    } catch (error) {
      if (isCurrentServer(server.id)) setReason(error instanceof Error ? error.message : String(error));
    } finally { setBusy(false); }
    if (isCurrentServer(server.id)) void connect();
  };
  const changeMode = (next: ScreenPointerMode) => {
    setMode(next);
    toViewer({ type: "mode", mode: next });
  };

  const title = status?.name || serverName;
  const menuItems: ActionMenuItem[] = [
    {
      key: "mode", icon: mode === "touch" ? "hand" : "navigate",
      label: mode === "touch" ? "Use as trackpad" : "Touch to click",
      detail: mode === "touch" ? "Move a pointer with your finger" : "Tap where you want to click",
      onPress: () => changeMode(mode === "touch" ? "trackpad" : "touch"),
    },
    { key: "fit", icon: "expand", label: "Fit screen", onPress: () => toViewer({ type: "fit" }) },
    ...(status?.approved ? [{
      key: "awake", icon: "power", disabled: busy,
      label: status.keep_awake ? "Let this computer sleep" : "Keep this computer awake",
      detail: status.keep_awake ? "It can't be reached while asleep" : "The display may still sleep",
      onPress: () => void setKeepAwake(!status.keep_awake),
    } satisfies ActionMenuItem] : []),
    { key: "stats", icon: "pulse", label: showStats ? "Hide connection stats" : "Show connection stats", onPress: () => setShowStats(!showStats) },
    phase === "live"
      ? { key: "disconnect", icon: "exit", label: "Disconnect", onPress: () => { disconnect(); setReason("Disconnected."); setPhase("lost"); } }
      : { key: "reconnect", icon: "refresh", label: "Reconnect", onPress: () => void connect() },
  ];
  const header = (
    <Stack.Screen options={{
      title,
      headerShown: !landscape,
      headerRight: () => currentServer ? (
        <IconButton icon="more-horizontal" size={40} accessibilityLabel="Screen options" onPress={() => setMenu(true)} />
      ) : null,
    }} />
  );
  const sheet = <ActionMenu visible={menu} title={title} items={menuItems} onClose={() => setMenu(false)} />;

  if (!currentServer) {
    return (
      <View style={[styles.root, styles.center, { backgroundColor: colors.bgPrimary }]}>
        <EmptyState icon="server" title="No current server" detail="Choose one in Settings." action={{ label: "Open Settings", onPress: () => router.push("/settings") }} />
      </View>
    );
  }

  const warning = powerWarning(status);
  const quality = connectionQuality(rtt);
  const qualityColor = quality === "good" ? colors.success : quality === "fair" ? colors.warning : quality === "poor" ? colors.dangerText : colors.textTertiary;
  const power = powerSummary(status?.power, status?.keep_awake ?? false);
  const viewerVisible = phase === "connecting" || phase === "live";
  const active = activeModifiers(mods);

  return (
    <View style={[styles.root, { backgroundColor: colors.bgPrimary, paddingLeft: landscape ? insets.left : 0, paddingRight: landscape ? insets.right : 0 }]}>
      {header}
      {sheet}
      {phase === "setup" && status ? (
        <ScrollView contentContainerStyle={[styles.center, styles.setup]}>
          <ScreenSetup status={status} name={title} busy={busy} reason={reason}
            onEnable={() => void enable()} onOpenSettings={(id) => void openSettings(id)}
            onRestart={() => void restartMewla()} onCheck={() => void connect()} />
        </ScrollView>
      ) : null}
      {phase === "checking" && !status ? (
        <View style={[styles.root, styles.center]}><EmptyState busy title={`Opening ${serverName}'s screen…`} /></View>
      ) : null}
      {phase === "lost" ? (
        <View style={styles.notice}>
          <InlineNotice tone="warning" title="Not connected to the screen" detail={reason}
            action={{ label: "Reconnect", onPress: () => void connect() }} />
        </View>
      ) : null}
      {warning && phase !== "setup" ? (
        <View style={styles.notice}>
          <InlineNotice tone="warning" icon="power" title={warning.title} detail={warning.detail}
            action={status && !status.keep_awake ? { label: "Keep awake", onPress: () => void setKeepAwake(true), disabled: busy } : undefined} />
        </View>
      ) : null}
      <View style={[styles.root, !viewerVisible && phase !== "lost" ? styles.hidden : null]}>
        <ScreenViewerHost ref={host} onMessage={onViewerMessage} backgroundColor={colors.bgElevated}
          accessibilityLabel={`Screen of ${title}`} />
        {phase === "connecting" ? (
          <View pointerEvents="none" style={[styles.center, StyleSheet.absoluteFill]}>
            <EmptyState busy title="Connecting…" />
          </View>
        ) : null}
        {showStats && phase === "live" ? (
          <Pressable accessibilityRole="button" accessibilityLabel="Hide connection stats" onPress={() => setShowStats(false)}
            style={[styles.stats, { backgroundColor: colors.bgSurface, borderColor: colors.borderSubtle, top: landscape ? Math.max(insets.top, Spacing.sm) : Spacing.sm }]}>
            <View style={[styles.dot, { backgroundColor: qualityColor }]} />
            <AppText variant="micro" style={{ color: colors.textSecondary }}>
              {formatStats({ rttMs: rtt, inputMs: stats?.inputMs ?? null, fps: stats?.fps ?? 0, kbps: stats?.kbps ?? 0 })}
              {power ? `\n${power}` : ""}
            </AppText>
          </Pressable>
        ) : null}
      </View>
      {phase === "live" ? (
        <View style={[styles.tray, { borderTopColor: colors.borderSubtle, paddingBottom: typing ? Spacing.xs : Math.max(insets.bottom, Spacing.xs) }]}>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} keyboardShouldPersistTaps="always" contentContainerStyle={styles.keys}>
            <KeyButton label="" icon="keyboard" name={typing ? "Hide keyboard" : "Show keyboard"} active={typing} onPress={toggleKeyboard} />
            {landscape ? <KeyButton label="" icon="more-horizontal" name="Screen options" onPress={() => setMenu(true)} /> : null}
            {MODIFIERS.map((m) => (
              <KeyButton key={m.key} label={m.label} name={m.name} active={active.includes(m.key)} locked={mods.locked.includes(m.key)}
                onPress={() => setMods(tapModifier(modsRef.current, m.key))} />
            ))}
            {KEYS.map((k) => <KeyButton key={k.key} label={k.label} name={k.name} onPress={() => sendKey(k.key)} />)}
            {SHORTCUTS.map((s) => (
              <KeyButton key={s.label} label={s.label} name={s.name} wide
                onPress={() => (s.key ? sendKey(s.key, s.mods) : sendText(s.text ?? "", s.mods))} />
            ))}
          </ScrollView>
          {/* Off-screen field that only collects soft-keyboard keystrokes. */}
          <TextInput ref={input} value={KEYBOARD_SENTINEL} onChangeText={onKeyboardText}
            onSubmitEditing={() => sendKey("Return")} submitBehavior="submit"
            onKeyPress={(event) => {
              // Hardware keyboards on web report keys the field can't show.
              const named: Record<string, ScreenKey> = { ArrowLeft: "Left", ArrowRight: "Right", ArrowUp: "Up", ArrowDown: "Down", Escape: "Escape", Tab: "Tab" };
              const key = named[event.nativeEvent.key];
              if (key && Platform.OS === "web") { event.preventDefault?.(); sendKey(key); }
            }}
            onBlur={() => setTyping(false)}
            autoCorrect={false} autoCapitalize="none" autoComplete="off" spellCheck={false}
            keyboardType={Platform.OS === "android" ? "visible-password" : "default"}
            accessibilityLabel="Type on the remote screen" style={styles.hiddenInput} caretHidden />
        </View>
      ) : null}
    </View>
  );
}

/**
 * The one next step before a viewer can open, the same on every platform:
 * unsupported, turned off at the computer, not yet allowed, or waiting for
 * operating-system permissions.
 */
function ScreenSetup({ status, name, busy, reason, onEnable, onOpenSettings, onRestart, onCheck }: {
  status: ScreenStatus;
  name: string;
  busy: boolean;
  reason: string | null;
  onEnable(): void;
  onOpenSettings(permission: string): void;
  onRestart(): void;
  onCheck(): void;
}) {
  const { colors } = useAppTheme();
  if (!status.supported) {
    return <EmptyState icon="desktop" title="Screen isn't available for this computer yet"
      detail={status.reason || `${name} can't share its screen with Mewla yet.`} />;
  }
  if (status.disabled) {
    return <EmptyState icon="lock" title="Screen is turned off on this computer"
      detail={`To allow devices again, run mewla screen enable on ${name}.`}
      action={{ label: "Check again", icon: "refresh", onPress: onCheck }} />;
  }
  if (!status.approved) {
    return (
      <View style={styles.setupBlock}>
        <EmptyState icon="desktop" title={busy ? `Waiting for Allow on ${name}…` : "Turn on Screen"}
          detail={busy
            ? "Someone at the computer has to click Allow. It also asks for the system permissions Screen needs."
            : `See and control ${name} from here. ${name} asks once whether to allow this device, so someone has to be at it.`}
          action={{ label: busy ? "Waiting…" : "Turn on Screen", icon: "desktop", onPress: onEnable, loading: busy }} />
        {reason && !busy ? <InlineNotice tone="warning" title="Not turned on" detail={reason} /> : null}
      </View>
    );
  }
  const missing = (status.permissions ?? []).filter((p) => !p.granted);
  // Some grants apply only to a new Mewla process on the computer.
  const restart = missing.some((p) => p.restart_required) && status.can_restart;
  return (
    <View style={styles.setupBlock}>
      <EmptyState icon="shield-check" title={`Allow Screen on ${name}`}
        detail={`${name} still needs these permissions. Approve its prompts, or open the settings there and turn Mewla on.`} />
      {missing.map((p) => (
        <View key={p.id} style={[styles.permission, { borderColor: colors.borderSubtle, backgroundColor: colors.bgSurface }]}>
          <View style={styles.permissionText}>
            <AppText variant="label" style={{ color: colors.textPrimary }}>{p.name}</AppText>
            {p.guidance ? <AppText variant="caption" style={{ color: colors.textSecondary }}>{p.guidance}</AppText> : null}
          </View>
          {p.can_open && !p.restart_required
            ? <Button label="Open settings" size="sm" variant="tinted" onPress={() => onOpenSettings(p.id)} /> : null}
        </View>
      ))}
      {restart
        ? <Button label={busy ? "Restarting Mewla…" : "Restart Mewla"} icon="refresh" loading={busy} onPress={onRestart} />
        : <Button label="Check again" icon="refresh" variant="outlined" onPress={onCheck} />}
      {reason ? <InlineNotice tone="warning" title="Something went wrong" detail={reason} /> : null}
    </View>
  );
}

function KeyButton({ label, name, icon, active, locked, wide, onPress }: {
  label: string;
  name: string;
  icon?: "keyboard" | "more-horizontal";
  active?: boolean;
  locked?: boolean;
  wide?: boolean;
  onPress(): void;
}) {
  const { colors } = useAppTheme();
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={name} accessibilityState={{ selected: !!active }}
      onPress={onPress} hitSlop={4}
      style={({ pressed }) => [styles.key, wide && styles.wideKey, {
        backgroundColor: active ? colors.accentSoft : pressed ? colors.surfaceSubtle : colors.bgSurface,
        borderColor: locked ? colors.accent : colors.borderSubtle,
        borderWidth: locked ? 2 : StyleSheet.hairlineWidth,
      }]}>
      {icon ? <Icon name={icon} size={18} color={active ? colors.accentStrong : colors.textSecondary} /> : (
        <AppText variant="label" style={{ color: active ? colors.accentStrong : colors.textPrimary }}>{label}</AppText>
      )}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  hidden: { display: "none" },
  center: { justifyContent: "center", alignItems: "center", padding: Spacing.lg },
  notice: { paddingHorizontal: Spacing.md, paddingTop: Spacing.sm },
  stats: {
    position: "absolute", left: Spacing.sm, flexDirection: "row", alignItems: "center", gap: Spacing.xs,
    paddingHorizontal: Spacing.sm, paddingVertical: 4, borderRadius: Radii.sm, borderWidth: StyleSheet.hairlineWidth, opacity: 0.92,
    ...ContinuousCorners,
  },
  dot: { width: 8, height: 8, borderRadius: 4 },
  tray: { borderTopWidth: StyleSheet.hairlineWidth, paddingTop: Spacing.xs },
  keys: { gap: Spacing.xs, paddingHorizontal: Spacing.sm, alignItems: "center" },
  key: { minWidth: TouchTarget, height: TouchTarget, paddingHorizontal: Spacing.sm, alignItems: "center", justifyContent: "center", borderRadius: Radii.sm, ...ContinuousCorners },
  wideKey: { paddingHorizontal: Spacing.md },
  setup: { flexGrow: 1 },
  setupBlock: { width: "100%", maxWidth: 440, gap: Spacing.md, alignItems: "stretch" },
  permission: {
    flexDirection: "row", alignItems: "center", gap: Spacing.md, padding: Spacing.md,
    borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.md, ...ContinuousCorners,
  },
  permissionText: { flex: 1, gap: 2 },
  hiddenInput: { position: "absolute", width: 1, height: 1, opacity: 0, left: -100, bottom: 0 },
});
