import React, { useCallback, useEffect, useRef, useState } from "react";
import { AppState, ScrollView, StyleSheet, View, type ScrollViewInstance } from "react-native";
import { Stack, useLocalSearchParams, useRouter } from "expo-router";
import { ContentWidth, Spacing, useAppTheme } from "../../constants/tokens";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkers } from "../../store/workers";
import { usePhoneUse } from "../../store/phoneUse";
import { haltDisplay, listDisplays, resumeDisplay, type AgentDisplay } from "../../services/display";
import { browserIssue, type BrowserIssue } from "../../services/browserPageModel";
import { serverDisplayName } from "../../services/serverDisplayName";
import { ActionMenu, EmptyState, IconButton, InlineNotice } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { AgentDisplayCard } from "./AgentDisplayCard";
import { phoneMenuItem } from "./phoneRow";

/** How often the list reads the computer again, so an agent that starts or ends shows up. */
const REFRESH_MS = 4000;

/**
 * Screen on a Linux computer: every agent using the agent screen (its browser
 * window and its desktop apps share one monitor), each with its own live view
 * and Watch, Take over, Stop. The rows of
 * other surfaces (the Phone) sit in the same menu.
 */
export function AgentScreens() {
  const router = useRouter();
  // /screen?worker=<id> (the floating screen's Expand) opens on that agent's row.
  const { worker: focusWorker } = useLocalSearchParams<{ worker?: string }>();
  const scroller = useRef<ScrollViewInstance>(null);
  const rowTops = useRef<Record<string, number>>({});
  const focused = useRef<string | null>(null);
  const focusRow = useCallback((id: string | undefined) => {
    const top = id ? rowTops.current[id] : undefined;
    if (id && top !== undefined && focused.current !== id) {
      focused.current = id;
      scroller.current?.scrollTo({ y: Math.max(0, top - Spacing.md), animated: false });
    }
  }, []);
  useEffect(() => { focused.current = null; focusRow(typeof focusWorker === "string" ? focusWorker : undefined); }, [focusWorker, focusRow]);
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { colors } = useAppTheme();
  const { state: workerState } = useWorkers();
  const phone = usePhoneUse();
  const serverName = serverDisplayName(currentServer?.name);
  const [displays, setDisplays] = useState<AgentDisplay[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [reason, setReason] = useState("");
  const [issue, setIssue] = useState<BrowserIssue | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [menu, setMenu] = useState(false);
  const abort = useRef<AbortController | null>(null);
  const fail = useCallback((error: unknown) => setIssue(browserIssue(error, serverName)), [serverName]);

  const list = useCallback(async () => {
    if (!currentServer) return null;
    const server = currentServer;
    const result = await listDisplays(server, abort.current?.signal);
    if (!isCurrentServer(server.id)) return null;
    setDisplays(result.displays);
    setReason(result.reason || "");
    setLoaded(true);
    return { displays: result.displays, available: !result.reason };
  }, [currentServer, isCurrentServer]);

  useEffect(() => {
    setDisplays([]);
    setLoaded(false);
    setReason("");
    setIssue(null);
    const controller = new AbortController();
    abort.current = controller;
    const read = () => { void list().catch((error) => { if (!controller.signal.aborted) { setLoaded(true); fail(error); } }); };
    read();
    const timer = setInterval(() => { if (AppState.currentState === "active") read(); }, REFRESH_MS);
    return () => { controller.abort(); clearInterval(timer); };
  }, [currentServer?.id, list, fail]);

  const run = async (key: string, action: () => Promise<unknown>) => {
    setBusy(key);
    try { await action(); } catch (error) { fail(error); } finally { setBusy(null); }
  };

  const menuItems: ActionMenuItem[] = [
    ...(phoneMenuItem(phone, () => router.push("/phone")) ? [phoneMenuItem(phone, () => router.push("/phone"))!] : []),
  ];
  const header = (
    <Stack.Screen options={{
      title: "Screen",
      headerRight: () => currentServer && menuItems.length > 0 ? (
        <IconButton icon="more-vertical" size={40} accessibilityLabel="Screen options" onPress={() => setMenu(true)} />
      ) : null,
    }} />
  );
  const sheet = <ActionMenu visible={menu} title="Screen" items={menuItems} onClose={() => setMenu(false)} />;

  if (!currentServer) {
    return (
      <View style={[styles.root, styles.center, { backgroundColor: colors.bgPrimary }]}>
        <EmptyState icon="server" title="No current server" detail="Choose one in Settings." action={{ label: "Open Settings", onPress: () => router.push("/settings") }} />
      </View>
    );
  }
  if (displays.length === 0) {
    const setupNeeded = Boolean(reason);
    return (
      <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
        {header}
        {sheet}
        {issue ? <View style={styles.notice}><InlineNotice tone={issue.tone} title={issue.title} detail={issue.detail} /></View> : null}
        <View style={[styles.root, styles.center]}>
          {!loaded ? (
            <EmptyState busy />
          ) : setupNeeded ? (
            <EmptyState icon="construct" title="Browser needs setup on this server" detail={reason}
              action={{ label: "Check again", icon: "refresh", onPress: () => void run("check", async () => { await list(); }), loading: busy === "check" }} />
          ) : (
            <EmptyState icon="desktop" title="No agent is using a screen"
              detail={`When an agent opens a browser it shows here. Take it over to sign in once; sign-ins are kept on ${serverName}.`} />
          )}
        </View>
      </View>
    );
  }
  return (
    <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
      {header}
      {sheet}
      <ScrollView ref={scroller} contentContainerStyle={styles.list}>
        <View style={styles.inner}>
          {issue ? <InlineNotice tone={issue.tone} title={issue.title} detail={issue.detail} /> : null}
          {displays.map((display) => (
            <View key={`display:${display.id}`} onLayout={(e) => {
              rowTops.current[display.id] = e.nativeEvent.layout.y;
              if (typeof focusWorker === "string") focusRow(focusWorker);
            }}>
              <AgentDisplayCard server={currentServer} display={display}
                agentName={workerState.workers.find((w) => w.id === display.id)?.name}
                onStop={async (worker) => { await haltDisplay(currentServer, worker, abort.current?.signal); await list(); }}
                onResume={async (worker) => { await resumeDisplay(currentServer, worker, abort.current?.signal); await list(); }} />
            </View>
          ))}
        </View>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  center: { justifyContent: "center", padding: Spacing.lg },
  notice: { paddingHorizontal: Spacing.md, paddingTop: Spacing.sm },
  list: { alignItems: "center", padding: Spacing.md },
  inner: { width: "100%", maxWidth: ContentWidth.page, gap: Spacing.md },
});
