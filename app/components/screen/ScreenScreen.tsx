import React, { useCallback, useEffect, useRef, useState } from "react";
import {
  AppState,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  useWindowDimensions,
  View,
} from "react-native";
import { Stack, useFocusEffect, useRouter } from "expo-router";
import { StatusBar } from "expo-status-bar";
import * as Haptics from "expo-haptics";
import * as ScreenOrientation from "expo-screen-orientation";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ContinuousCorners, Radii, Spacing, TouchTarget, useAppTheme } from "../../constants/tokens";
import { hasRoomyHeight } from "../../constants/windowRoom";
import { useCurrentServer } from "../../store/currentServer";
import { useIPhoneUse } from "../../store/iphoneUse";
import { currentTransportPath, directPathBecameAvailable, directPathHint, recheckDirectPath, type TransportPath } from "../../services/pinnedTransport";
import { ScreenConnection, screenRequest, type ScreenStatus } from "../../services/screen";
import type { ScreenStreamStats } from "../../services/screenStream";
import { useDesktopWeb } from "../navigation/useDesktopWeb";
import { ActionMenu, BottomSheetFrame, Button, EmptyState, IconButton } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { AppText } from "../ui/AppText";
import { Icon } from "../icons/Icon";
import { MAC_MODIFIERS, needsSoftKeyboard, RemoteView, type RemoteViewHandle } from "./RemoteView";
import { AgentScreens } from "./AgentScreens";
import { phoneMenuItem } from "./phoneRow";
import { usePhoneUse } from "../../store/phoneUse";
import { useVirtualScreenLabs } from "../../store/virtualScreenLabs";
import { VirtualScreenSwitch } from "../settings/VirtualScreenSettings";
import { warnOnce } from "../../services/warnOnce";
import { serverDisplayName } from "../../services/serverDisplayName";
import {
  connectionProblem,
  diagnosticRows,
  indicatorTone,
  nextSlowStreak,
  powerSummary,
  powerWarning,
  screenAgents,
  screenEntry,
  slowLasts,
  unsupportedScreen,
  type IndicatorTone,
  type StreamReadout,
} from "./screenPageModel";
import { LIVE_DEADLINE_REASON, startLiveDeadline } from "./screenLiveDeadline";
import type { ScreenPointerMode, ScreenViewerMessage } from "./screenViewerProtocol";

type Phase = "checking" | "setup" | "agent" | "local" | "connecting" | "live" | "lost";

/**
 * The current server's own screen, through its Screen Sharing. Leaving,
 * backgrounding or switching servers closes the viewer; nothing reconnects
 * on its own except when the app returns to the foreground.
 */
export function ScreenScreen() {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { height } = useWindowDimensions();
  // Not enough height for a page header beside the picture (a phone held
  // sideways): the screen takes the whole display. Height decides, not
  // aspect ratio: an unfolded foldable is wider than tall yet roomy. A
  // desktop window keeps its page header.
  const desktopWeb = useDesktopWeb();
  const immersive = !hasRoomyHeight(height) && !desktopWeb;
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { colors } = useAppTheme();
  const serverName = serverDisplayName(currentServer?.name);
  const [phase, setPhase] = useState<Phase>("checking");
  const [status, setStatus] = useState<ScreenStatus | null>(null);
  const [reason, setReason] = useState<string | null>(null);
  const [rtt, setRtt] = useState<number | null>(null);
  const [stats, setStats] = useState<{ fps: number; kbps: number; inputMs: number | null; bridgeMs?: number | null; presentMs?: number | null; width: number; height: number } | null>(null);
  // What the daemon measured of the stream, and whether the computer is locked.
  const [daemonStats, setDaemonStats] = useState<ScreenStreamStats | null>(null);
  const [locked, setLocked] = useState(false);
  // The connection sheet behind the indicator, whether slowness has lasted,
  // and the one brief note about it per connection.
  const [details, setDetails] = useState(false);
  const [slow, setSlow] = useState(false);
  const [slowNote, setSlowNote] = useState(false);
  const [mode, setMode] = useState<ScreenPointerMode>("touch");
  const [typing, setTyping] = useState(false);
  const [menu, setMenu] = useState(false);
  const [busy, setBusy] = useState(false);
  const iphone = useIPhoneUse(5_000);
  const phone = usePhoneUse();
  const virtualScreen = useVirtualScreenLabs();
  // Coming back from Settings, where the switch may have changed.
  const refreshVirtualScreen = virtualScreen.refresh;
  useFocusEffect(useCallback(() => { refreshVirtualScreen(); }, [refreshVirtualScreen]));
  const [watching, setWatching] = useState(false);
  // Which agent is being watched, when several work at once.
  const [watchedWorker, setWatchedWorker] = useState<string | undefined>();
  const view = useRef<RemoteViewHandle>(null);
  const connection = useRef<ScreenConnection | null>(null);
  // The path the live viewer opened on, and whether it has retried its loss.
  const openedVia = useRef<TransportPath | null>(null);
  const [viaPath, setViaPath] = useState<TransportPath | null>(null);
  const retriedLostPath = useRef(false);
  const epoch = useRef(0);
  // Ends the wait for a picture: a connection that never goes live is lost, with Reconnect.
  const liveDeadline = useRef<(() => void) | null>(null);
  const slowStreak = useRef(0);
  const saidSlow = useRef(false);
  const readoutRef = useRef<StreamReadout | null>(null);

  const disconnect = useCallback(() => {
    epoch.current++;
    liveDeadline.current?.();
    liveDeadline.current = null;
    connection.current?.close();
    connection.current = null;
    view.current?.close();
    setRtt(null);
    setStats(null);
    setDaemonStats(null);
    setSlowNote(false);
  }, []);

  // A person comes first, so viewing ends an agent's control. Only an
  // explicit "takeOver" does that; opening the page waits for the agent, and
  // "watch" shows the agent's screen without taking over.
  const connect = useCallback(async (how: "auto" | "takeOver" | "watch" = "auto", worker?: string) => {
    if (!currentServer) return;
    const server = currentServer;
    disconnect();
    setWatching(false);
    saidSlow.current = false;
    slowStreak.current = 0;
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
    setLocked(!!next.locked);
    // Unsupported, off at the computer, not yet allowed, or missing
    // permissions: the setup view explains the one next step.
    const entry = screenEntry(next, how);
    if (entry === "setup") {
      setReason(next.reason || null);
      setPhase("setup");
      return;
    }
    // On the computer itself the screen is the one in front of the person: no live view, no stream.
    if (entry === "local" || entry === "agent") {
      setPhase(entry);
      return;
    }
    const watch = how === "watch" && !!next.agent;
    setWatching(watch);
    setWatchedWorker(watch ? worker : undefined);
    setPhase("connecting");
    liveDeadline.current = startLiveDeadline(() => {
      if (!active()) return;
      disconnect();
      setReason(LIVE_DEADLINE_REASON);
      setPhase("lost");
    });
    const socket = new ScreenConnection(server, {
      open() {
        if (!active()) return;
        openedVia.current = socket.path;
        setViaPath(socket.path);
        view.current?.open();
      },
      data(chunk) {
        if (!active()) return;
        retriedLostPath.current = false;
        view.current?.feed(chunk);
      },
      ready() {},
      rtt(ms) { if (active()) setRtt(ms); },
      locked(now) { if (active()) setLocked(now); },
      streamStats(next) { if (active()) setDaemonStats(next); },
      closed(why) {
        if (!active()) return;
        view.current?.close();
        // The agent finished: look again, which opens the screen.
        if (watch) { void connect(); return; }
        // A viewer that was on a direct path when it ended retries once. The
        // retry first re-verifies that path, so a lost one goes to the relay.
        if (openedVia.current && openedVia.current !== "relay" && !retriedLostPath.current) {
          retriedLostPath.current = true;
          recheckDirectPath(server.id);
          void connect();
          return;
        }
        setReason(why || null);
        setPhase("lost");
      },
    }, watch, worker);
    connection.current = socket;
    try { await socket.connect(); } catch (error) {
      if (!active()) return;
      setReason(error instanceof Error ? error.message : String(error));
      setPhase("lost");
    }
  }, [currentServer, disconnect, isCurrentServer]);

  useEffect(() => {
    setStatus(null);
    void connect();
    const subscription = AppState.addEventListener("change", (state) => {
      if (state === "active") void connect();
      else disconnect();
    });
    return () => { subscription.remove(); disconnect(); };
  }, [currentServer?.id, connect, disconnect]);

  // A viewer opened on the relay moves to a direct path once one is verified,
  // with one quick reconnect, at most once a minute.
  const lastMove = useRef(0);
  useEffect(() => {
    if (!currentServer || phase !== "live" || watching) return;
    const server = currentServer;
    let checking = false;
    const timer = setInterval(() => {
      if (checking || openedVia.current !== "relay" || Date.now() - lastMove.current < 60_000) return;
      checking = true;
      const now = currentTransportPath(server.id);
      void (now && now !== "relay" ? Promise.resolve(true) : directPathBecameAvailable(server))
        .then((ready) => {
          if (!ready || !isCurrentServer(server.id) || openedVia.current !== "relay") return;
          lastMove.current = Date.now();
          void connect();
        })
        .catch(() => undefined)
        .finally(() => { checking = false; });
    }, 5000);
    return () => clearInterval(timer);
  }, [currentServer, phase, watching, isCurrentServer, connect]);

  // This page asks to rotate and, on leaving, for portrait again. Both are
  // preferences the system may refuse (iOS 27, foldables): nothing here
  // assumes the orientation, the layout follows the window.
  useEffect(() => {
    if (Platform.OS === "web") return;
    void ScreenOrientation.unlockAsync().catch(() => {});
    return () => { void ScreenOrientation.lockAsync(ScreenOrientation.OrientationLock.PORTRAIT_UP).catch(() => {}); };
  }, []);

  // While an allowed device waits for permissions, follow the computer: the
  // person there turns switches on, and Mewla may restart to apply them.
  const waitingForPermissions = phase === "setup" && !!status?.supported && !status.disabled && status.approved && !status.ready;
  useEffect(() => {
    if (!waitingForPermissions || !currentServer) return;
    const server = currentServer;
    const timer = setInterval(() => {
      void screenRequest(server).then((next) => {
        if (!isCurrentServer(server.id)) return;
        setStatus(next);
        if (next.ready) void connect();
      }).catch(() => { /* restarting */ });
    }, 3000);
    return () => clearInterval(timer);
  }, [connect, currentServer, isCurrentServer, waitingForPermissions]);

  // While an agent uses the screen, look again until it is done, then open. On the computer itself
  // the card just follows the agents.
  useEffect(() => {
    if ((phase !== "agent" && phase !== "local") || !currentServer) return;
    const server = currentServer;
    const timer = setInterval(() => {
      void screenRequest(server).then((next) => {
        if (!isCurrentServer(server.id)) return;
        setStatus(next);
        if (phase === "agent" && !next.agent) void connect();
      }).catch((error) => warnOnce("screen.status", error));
    }, 3000);
    return () => clearInterval(timer);
  }, [connect, currentServer, isCurrentServer, phase]);

  // Power state changes slowly; refresh it while the viewer is open.
  useEffect(() => {
    if (phase !== "live" || !currentServer) return;
    const server = currentServer;
    const timer = setInterval(() => {
      void screenRequest(server).then((next) => { if (isCurrentServer(server.id)) setStatus(next); }).catch((error) => warnOnce("screen.status", error));
    }, 30000);
    return () => clearInterval(timer);
  }, [currentServer, isCurrentServer, phase]);

  // Slowness counts only when it lasts, so one spike never lights the
  // indicator; it is said once per connection, briefly, and stays in the sheet.
  useEffect(() => {
    const readout = readoutRef.current;
    if (rtt === null || !readout) {
      slowStreak.current = 0;
      setSlow(false);
      return;
    }
    slowStreak.current = nextSlowStreak(slowStreak.current, connectionProblem(readout) !== null);
    const lasts = slowLasts(slowStreak.current);
    setSlow(lasts);
    if (lasts && !saidSlow.current) {
      saidSlow.current = true;
      setSlowNote(true);
    }
  }, [rtt]);
  useEffect(() => {
    if (!slowNote) return;
    const timer = setTimeout(() => setSlowNote(false), 8000);
    return () => clearTimeout(timer);
  }, [slowNote]);

  const onEvent = useCallback((message: ScreenViewerMessage) => {
    switch (message.type) {
      case "closed":
        // The RFB client gave up (protocol error); end the socket too.
        if (connection.current) {
          connection.current.close();
          connection.current = null;
          setReason(null);
          setPhase("lost");
        }
        break;
      case "connected":
        liveDeadline.current?.();
        liveDeadline.current = null;
        setPhase("live");
        break;
      case "stats": setStats(message); break;
      case "haptic": if (Platform.OS !== "web") void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium).catch(() => {}); break;
    }
  }, []);
  const onSend = useCallback((data: string) => connection.current?.send(data), []);
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
  const stopAgent = async (worker?: string) => {
    if (!currentServer) return;
    const server = currentServer;
    setBusy(true);
    try {
      const next = await screenRequest(server, worker ? { action: "stop_agent", worker } : { action: "stop_agent" });
      if (isCurrentServer(server.id)) setStatus(next);
    } catch (error) {
      if (isCurrentServer(server.id)) setReason(error instanceof Error ? error.message : String(error));
    } finally { setBusy(false); }
    if (isCurrentServer(server.id)) void connect();
  };
  const changeMode = (next: ScreenPointerMode) => {
    setMode(next);
  };

  const title = status?.name || serverName;
  const warning = phase === "live" ? powerWarning(status) : null;
  const power = powerSummary(status?.power, status?.keep_awake ?? false);
  const readout: StreamReadout = {
    rttMs: rtt, inputMs: stats?.inputMs ?? null, fps: stats?.fps ?? 0, kbps: stats?.kbps ?? 0,
    bridgeMs: stats?.bridgeMs, presentMs: stats?.presentMs, daemon: daemonStats,
    viaPath,
    directHint: currentServer && viaPath === "relay" ? directPathHint(currentServer) : null,
  };
  readoutRef.current = readout;
  const problem = slow ? connectionProblem(readout) : null;
  const tone = indicatorTone(!!problem, warning);
  const viewerVisible = phase === "connecting" || phase === "live";
  // Held sideways the picture owns the display: no header, no status bar.
  const fullBleed = immersive && viewerVisible;
  const agents = screenAgents(status);
  // An agent on the iPhone works in iPhone Mirroring, off the Mac's screen: Watch can't show it,
  // so it has its own Done (for what it asked of the person) and Stop.
  const iphoneAgent = iphone.status?.enabled ? iphone.status.active : undefined;
  const agentLabel = (watchedWorker ? agents.find((a) => a.worker === watchedWorker)?.name : undefined) || status?.agent?.name || "An agent";
  const keepAwakeItem: ActionMenuItem[] = status?.approved ? [{
    key: "awake", icon: "power", disabled: busy,
    label: status.keep_awake ? "Let this computer sleep" : "Keep this computer awake",
    onPress: () => void setKeepAwake(!status.keep_awake),
  }] : [];
  const menuItems: ActionMenuItem[] = phase === "local" ? [
    ...keepAwakeItem,
    ...(phoneMenuItem(phone, () => router.push("/phone")) ? [phoneMenuItem(phone, () => router.push("/phone"))!] : []),
  ] : watching ? [
    { key: "fit", icon: "expand", label: "Fit screen", onPress: () => view.current?.fit() },
    { key: "disconnect", icon: "exit", label: "Stop watching", onPress: () => { disconnect(); setWatching(false); setPhase("agent"); } },
  ] : [
    {
      key: "mode", icon: mode === "touch" ? "hand" : "navigate",
      label: mode === "touch" ? "Use as trackpad" : "Touch to click",
      onPress: () => changeMode(mode === "touch" ? "trackpad" : "touch"),
    },
    { key: "fit", icon: "expand", label: "Fit screen", onPress: () => view.current?.fit() },
    ...(needsSoftKeyboard() ? [] : [{ key: "full", icon: "expand", label: "Full size", onPress: () => view.current?.fullSize() } as const]),
    ...keepAwakeItem,
    ...(phoneMenuItem(phone, () => router.push("/phone")) ? [phoneMenuItem(phone, () => router.push("/phone"))!] : []),
    phase === "live"
      ? { key: "disconnect", icon: "exit", label: "Disconnect", onPress: () => { disconnect(); setReason(null); setPhase("lost"); } }
      : { key: "reconnect", icon: "refresh", label: "Reconnect", onPress: () => void connect() },
  ];
  const keyboardButton = phase === "live" && needsSoftKeyboard();
  const indicator = phase === "live" ? (
    <ConnectionIndicator tone={tone} locked={locked} onPress={() => setDetails(true)} />
  ) : null;
  const header = (
    <Stack.Screen options={{
      title,
      headerShown: !fullBleed,
      headerRight: () => currentServer ? (
        <View style={styles.headerActions}>
          {slowNote && problem ? <AppText variant="micro" style={{ color: colors.warning }}>{problem.title}</AppText> : null}
          {indicator}
          {keyboardButton ? (
            <IconButton icon="keyboard" size={40} tone={typing ? "tinted" : "default"} accessibilityLabel={typing ? "Hide keyboard" : "Show keyboard"}
              onPress={() => view.current?.toggleKeyboard()} />
          ) : null}
          <IconButton icon="more-vertical" size={40} accessibilityLabel="Screen options" onPress={() => setMenu(true)} />
        </View>
      ) : null,
    }} />
  );
  const sheet = <ActionMenu visible={menu} title={title} items={menuItems} onClose={() => setMenu(false)} />;

  if (!currentServer) {
    return (
      <View style={[styles.root, styles.center, { backgroundColor: colors.bgPrimary }]}>
        <EmptyState icon="server" title="No current server" action={{ label: "Open Settings", onPress: () => router.push("/settings") }} />
      </View>
    );
  }

  // A Linux computer has no screen to share, but its agents' browsers run on a
  // virtual display of their own, shown here with the same Watch, Take over and Stop.
  if (virtualScreen.enabled && status && !status.supported && status.platform === "linux") {
    return <AgentScreens />;
  }

  const iphoneRow = iphoneAgent ? (
    <View style={styles.agentActions}>
      <AppText variant="body" style={styles.agentTitle}>{`${iphoneAgent.name || "An agent"} is using your iPhone`}</AppText>
      <Button label="Watch" icon="eye" size="sm" accessibilityLabel={`Watch ${iphoneAgent.name || "the agent"} on your iPhone`}
        onPress={() => router.push("/settings/iphone")} />
      <Button label="Done" icon="check" size="sm" accessibilityLabel="Done: tell the agent you finished on your iPhone"
        onPress={() => void iphone.change({ handoff_done: true })} disabled={iphone.busy} />
      <Button label="Stop" size="sm" variant="outlined" accessibilityLabel={`Stop ${iphoneAgent.name || "the agent"} on your iPhone`}
        onPress={() => void iphone.change({ stop_agent: true })} disabled={iphone.busy} />
    </View>
  ) : null;
  const takeOver = () => void connect("takeOver");
  const stopWatched = () => void stopAgent(watchedWorker);

  return (
    <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
      {header}
      {sheet}
      {Platform.OS !== "web" ? <StatusBar hidden={fullBleed} /> : null}
      <ConnectionSheet visible={details} onClose={() => setDetails(false)} problem={problem}
        rows={diagnosticRows(readout, power)} warning={warning} locked={locked} busy={busy}
        onKeepAwake={() => void setKeepAwake(true)} />
      {phase === "setup" && status ? (
        <ScrollView contentContainerStyle={[styles.center, styles.setup]}>
          <ScreenSetup status={status} name={title} busy={busy} reason={reason}
            onEnable={() => void enable()} onOpenSettings={(id) => void openSettings(id)}
            onRestart={() => void restartMewla()} onCheck={() => void connect()}
            onChooseComputer={() => router.push("/settings")} />
        </ScrollView>
      ) : null}
      {phase === "agent" && status?.agent ? (
        <View style={[styles.root, styles.center]}>
          <Icon name="desktop" size={40} color={colors.textTertiary} />
          <AppText variant="title" style={styles.agentTitle}>
            {agents.length > 1 ? `${agents.length} agents are using the screen` : `${agentLabel} is using the screen`}
          </AppText>
          {agents.map((agent) => {
            const label = agent.name || "An agent";
            return (
              <View key={agent.worker} style={styles.agentActions}>
                {agents.length > 1 ? <AppText variant="body" style={styles.agentTitle}>{label}</AppText> : null}
                <Button label="Watch" icon="eye" size="sm" accessibilityLabel={`Watch ${label}`} onPress={() => void connect("watch", agent.worker)} disabled={busy} />
                <Button label="Stop" size="sm" variant="outlined" accessibilityLabel={`Stop ${label}`} onPress={() => void stopAgent(agent.worker)} disabled={busy} />
              </View>
            );
          })}
          {iphoneRow}
          <View style={styles.agentActions}>
            <Button label={agents.length > 1 ? "Take over (stops all)" : "Take over"} icon="hand" size="sm" variant="filled" onPress={() => void connect("takeOver")} disabled={busy} />
          </View>
        </View>
      ) : null}
      {phase === "local" ? (
        <View style={[styles.root, styles.center]}>
          <View style={[styles.localCard, { backgroundColor: colors.bgSurface, borderColor: colors.borderSubtle }]}>
            {agents.length === 0 ? (
              <View style={styles.localRow}>
                <Icon name="desktop" size={20} color={colors.textTertiary} />
                <AppText variant="body" tone="secondary" style={styles.watchLabel}>No agent on this screen</AppText>
              </View>
            ) : agents.map((agent) => {
              const label = agent.name || "An agent";
              return (
                <View key={agent.worker} style={styles.localRow}>
                  <Icon name="desktop" size={20} color={colors.textSecondary} />
                  <AppText variant="body" numberOfLines={1} style={styles.watchLabel}>{label}</AppText>
                  <Button label="Stop" size="sm" variant="outlined" accessibilityLabel={`Stop ${label}`} onPress={() => void stopAgent(agent.worker)} disabled={busy} />
                </View>
              );
            })}
          </View>
          {iphoneRow}
        </View>
      ) : null}
      {phase === "checking" && !status ? (
        <View style={[styles.root, styles.center]}><EmptyState busy /></View>
      ) : null}
      <View style={[styles.root, styles.stage, !viewerVisible && phase !== "lost" ? styles.hidden : null]}>
        <View style={styles.root}>
          <RemoteView ref={view} style={styles.root} viewStyle={styles.root} backgroundColor={colors.bgElevated}
            accessibilityLabel={`Screen of ${title}`} align={immersive ? "center" : "top"} mode={mode} modifiers={MAC_MODIFIERS}
            onSend={onSend} onEvent={onEvent} onKeyboardChange={setTyping} bottomInset={insets.bottom} />
          {phase === "connecting" ? (
            <View pointerEvents="none" style={[styles.center, StyleSheet.absoluteFill]}>
              <EmptyState busy />
            </View>
          ) : null}
          {phase === "lost" ? (
            <View style={[styles.center, StyleSheet.absoluteFill, { backgroundColor: colors.bgPrimary }]}>
              <Icon name={locked ? "lock" : "cloud-offline"} size={40} color={colors.textTertiary} />
              <AppText variant="title" style={styles.agentTitle}>{locked ? "Mac is locked" : "Disconnected"}</AppText>
              {reason && !locked ? <AppText variant="caption" tone="secondary" numberOfLines={1} style={styles.agentTitle}>{reason}</AppText> : null}
              <Button label="Reconnect" icon="refresh" size="sm" variant="filled" onPress={() => void connect()} style={styles.reconnect} />
            </View>
          ) : null}
        </View>
        {fullBleed && phase === "live" ? (
          // Sideways, the controls stand beside the picture, which is wider
          // than tall and leaves that strip unused.
          <ScrollView style={[styles.rail, { borderLeftColor: colors.borderSubtle, backgroundColor: colors.bgPrimary }]}
            contentContainerStyle={styles.railContent} showsVerticalScrollIndicator={false}>
            {indicator}
            {watching ? (
              <>
                <RailButton icon="hand" name="Take over" color={colors.accentStrong} disabled={busy} onPress={takeOver} />
                <RailButton icon="stop" name={`Stop ${agentLabel}`} color={colors.dangerText} disabled={busy} onPress={stopWatched} />
              </>
            ) : null}
            {keyboardButton ? <RailButton icon="keyboard" name={typing ? "Hide keyboard" : "Show keyboard"} onPress={() => view.current?.toggleKeyboard()} /> : null}
            <RailButton icon="more-vertical" name="Screen options" onPress={() => setMenu(true)} />
          </ScrollView>
        ) : null}
      </View>
      {phase === "live" && watching && !immersive ? (
        <View style={[styles.watchBar, { borderTopColor: colors.borderSubtle, borderTopWidth: StyleSheet.hairlineWidth, paddingTop: Spacing.xs, paddingBottom: Spacing.xs }]}>
          <Icon name="eye" size={16} color={colors.textSecondary} />
          <AppText variant="caption" tone="secondary" numberOfLines={1} style={styles.watchLabel}>{`Watching ${agentLabel}`}</AppText>
          <Button label="Take over" icon="hand" size="sm" variant="filled" onPress={takeOver} disabled={busy} />
          <Button label="Stop" size="sm" variant="outlined" accessibilityLabel={`Stop ${agentLabel}`} onPress={stopWatched} disabled={busy} />
        </View>
      ) : null}
    </View>
  );
}

/**
 * The only standing trace of the connection: a dot that is nearly invisible
 * while all is well, amber when something needs attention, a lock when the
 * computer is locked. It sits in chrome, never on the picture; a tap opens
 * the details.
 */
function ConnectionIndicator({ tone, locked, onPress }: { tone: IndicatorTone; locked: boolean; onPress(): void }) {
  const { colors } = useAppTheme();
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={locked ? "Mac is locked, connection details" : tone === "warn" ? "Connection needs attention" : "Connection details"}
      onPress={onPress} style={styles.indicator}>
      {locked ? <Icon name="lock" size={14} color={colors.textTertiary} /> : (
        <View style={[styles.dot, tone === "warn" ? { backgroundColor: colors.warning } : { backgroundColor: colors.textTertiary, opacity: 0.45 }]} />
      )}
    </Pressable>
  );
}

/**
 * What stands behind the indicator: the problem and its way out, if there is
 * one, then every diagnostic once. None of it is ever drawn on the picture.
 */
function ConnectionSheet({ visible, onClose, problem, rows, warning, locked, busy, onKeepAwake }: {
  visible: boolean;
  onClose(): void;
  problem: ReturnType<typeof connectionProblem>;
  rows: ReturnType<typeof diagnosticRows>;
  warning: ReturnType<typeof powerWarning>;
  locked: boolean;
  busy: boolean;
  onKeepAwake(): void;
}) {
  const { colors } = useAppTheme();
  return (
    <BottomSheetFrame visible={visible} onClose={onClose} maxHeight="85%" desktopWidth={420} contentStyle={styles.sheet}>
      <AppText variant="sheetTitle" accessibilityRole="header">Connection</AppText>
      {problem ? (
        <View style={styles.sheetNote}>
          <AppText variant="label" style={{ color: colors.warning }}>{problem.title}</AppText>
          {problem.fix ? <AppText variant="caption" tone="secondary">{problem.fix}</AppText> : null}
        </View>
      ) : null}
      {locked ? <AppText variant="label" tone="secondary">Mac is locked</AppText> : null}
      {warning ? (
        <View style={styles.sheetNote}>
          <AppText variant="label" style={{ color: warning.label === "On battery" ? colors.textSecondary : colors.warning }}>
            {warning.action ? "This computer may sleep" : warning.label}
          </AppText>
          {warning.action ? <Button label="Keep awake" icon="power" size="sm" variant="tinted" onPress={onKeepAwake} disabled={busy} /> : null}
        </View>
      ) : null}
      <View style={styles.sheetRows}>
        {rows.map((row) => (
          <View key={row.label} style={styles.sheetRow}>
            <AppText variant="body" tone="secondary">{row.label}</AppText>
            <AppText variant="body" style={styles.sheetValue}>{row.value}</AppText>
          </View>
        ))}
      </View>
    </BottomSheetFrame>
  );
}

function RailButton({ icon, name, color, disabled, onPress }: {
  icon: "hand" | "stop" | "keyboard" | "more-vertical";
  name: string;
  color?: string;
  disabled?: boolean;
  onPress(): void;
}) {
  const { colors } = useAppTheme();
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={name} disabled={disabled} onPress={onPress} hitSlop={4}
      style={({ pressed }) => [styles.key, { backgroundColor: pressed ? colors.surfaceSubtle : colors.bgSurface, borderColor: colors.borderSubtle, borderWidth: StyleSheet.hairlineWidth, opacity: disabled ? 0.5 : 1 }]}>
      <Icon name={icon} size={18} color={color ?? colors.textSecondary} />
    </Pressable>
  );
}

/**
 * The one next step before a viewer can open, the same on every platform:
 * unsupported, turned off at the computer, not yet allowed, or waiting for
 * operating-system permissions.
 */
function ScreenSetup({ status, name, busy, reason, onEnable, onOpenSettings, onRestart, onCheck, onChooseComputer }: {
  status: ScreenStatus;
  name: string;
  busy: boolean;
  reason: string | null;
  onEnable(): void;
  onOpenSettings(permission: string): void;
  onRestart(): void;
  onCheck(): void;
  onChooseComputer(): void;
}) {
  const { colors } = useAppTheme();
  if (!status.supported && status.platform === "linux") {
    // No screen to share, but its agents' browsers can have one: the switch is the way in.
    return <View style={styles.setupBlock}><VirtualScreenSwitch /></View>;
  }
  if (!status.supported) {
    const unsupported = unsupportedScreen(status, Platform.OS !== "web");
    return <EmptyState icon="desktop" title={unsupported.title} detail={unsupported.detail}
      action={unsupported.canChooseComputer ? { label: "Choose a computer", icon: "swap-horizontal", onPress: onChooseComputer } : undefined} />;
  }
  if (status.disabled) {
    return <EmptyState icon="lock" title="Screen is off"
      detail={`Run mewla screen enable on ${name}.`}
      action={{ label: "Check again", icon: "refresh", onPress: onCheck }} />;
  }
  if (!status.approved) {
    return (
      <EmptyState icon="desktop" title={busy ? `Waiting for Allow on ${name}…` : undefined}
        detail={busy ? null : reason}
        action={{ label: busy ? "Waiting…" : "Turn on Screen", icon: "desktop", onPress: onEnable, loading: busy }} />
    );
  }
  const missing = (status.permissions ?? []).filter((p) => !p.granted);
  // Some grants apply only to a new Mewla process on the computer.
  const restart = missing.some((p) => p.restart_required) && status.can_restart;
  return (
    <View style={styles.setupBlock}>
      <EmptyState icon="shield-check" title={`Allow Screen on ${name}`}
        action={restart ? undefined : { label: `Ask on ${name}`, icon: "desktop", onPress: onEnable, loading: busy }} />
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
      {reason ? <AppText variant="caption" tone="danger" numberOfLines={2} style={styles.reasonLine}>{reason}</AppText> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  hidden: { display: "none" },
  center: { justifyContent: "center", alignItems: "center", padding: Spacing.lg },
  agentTitle: { textAlign: "center" },
  agentActions: { flexDirection: "row", flexWrap: "wrap", justifyContent: "center", gap: Spacing.sm, paddingTop: Spacing.sm },
  reconnect: { marginTop: Spacing.sm },
  reasonLine: { textAlign: "center" },
  // Nothing is drawn over the picture: sideways the controls stand beside it.
  stage: { flexDirection: "row" },
  rail: { flexGrow: 0, flexShrink: 0, width: TouchTarget + Spacing.xs * 2 + 1, borderLeftWidth: StyleSheet.hairlineWidth },
  railContent: { flexGrow: 1, justifyContent: "center", alignItems: "center", gap: Spacing.xs, paddingHorizontal: Spacing.xs, paddingVertical: Spacing.xs },
  headerActions: { flexDirection: "row", alignItems: "center", gap: Spacing.xs },
  indicator: { width: TouchTarget, height: TouchTarget, alignItems: "center", justifyContent: "center" },
  dot: { width: 8, height: 8, borderRadius: 4 },
  localCard: {
    alignSelf: "stretch", maxWidth: 440, width: "100%", gap: Spacing.sm, padding: Spacing.md,
    borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.md, ...ContinuousCorners,
  },
  localRow: { flexDirection: "row", alignItems: "center", gap: Spacing.sm },
  watchBar: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, paddingHorizontal: Spacing.md },
  watchLabel: { flex: 1 },
  sheet: { gap: Spacing.md, paddingHorizontal: Spacing.lg, paddingBottom: Spacing.lg },
  sheetNote: { gap: Spacing.xs, alignItems: "flex-start" },
  sheetRows: { gap: Spacing.sm },
  sheetRow: { flexDirection: "row", justifyContent: "space-between", gap: Spacing.md },
  sheetValue: { flexShrink: 1, textAlign: "right" },
  key: { minWidth: TouchTarget, height: TouchTarget, paddingHorizontal: Spacing.sm, alignItems: "center", justifyContent: "center", borderRadius: Radii.sm, ...ContinuousCorners },
  setup: { flexGrow: 1 },
  setupBlock: { width: "100%", maxWidth: 440, gap: Spacing.md, alignItems: "stretch" },
  permission: {
    flexDirection: "row", alignItems: "center", gap: Spacing.md, padding: Spacing.md,
    borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.md, ...ContinuousCorners,
  },
  permissionText: { flex: 1, gap: 2 },
});
