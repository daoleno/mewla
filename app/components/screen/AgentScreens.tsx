import React, { useCallback, useEffect, useRef, useState } from "react";
import { AppState, ScrollView, StyleSheet, View } from "react-native";
import { Stack, useRouter } from "expo-router";
import { ContentWidth, Spacing, useAppTheme } from "../../constants/tokens";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkers } from "../../store/workers";
import { usePhoneUse } from "../../store/phoneUse";
import { browserRequest, type BrowserResource } from "../../services/browser";
import { haltDisplay, listDisplays, resumeDisplay, type AgentDisplay } from "../../services/display";
import { browserIssue, type BrowserIssue } from "../../services/browserPageModel";
import { serverDisplayName } from "../../services/serverDisplayName";
import { ActionMenu, EmptyState, IconButton, InlineNotice } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { AgentBrowserCard } from "./AgentBrowserCard";
import { AgentDisplayCard } from "./AgentDisplayCard";
import { phoneMenuItem } from "./phoneRow";

/** How often the list reads the computer again, so an agent that starts or ends shows up. */
const REFRESH_MS = 4000;

/**
 * Screen on a Linux computer: every agent using a browser on its virtual
 * screen, each with its own live view and Watch, Take over, Stop. The rows of
 * other surfaces (the Phone) sit in the same menu.
 */
export function AgentScreens() {
  const router = useRouter();
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { colors } = useAppTheme();
  const { state: workerState } = useWorkers();
  const phone = usePhoneUse();
  const serverName = serverDisplayName(currentServer?.name);
  const [resources, setResources] = useState<BrowserResource[]>([]);
  const [displays, setDisplays] = useState<AgentDisplay[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [reason, setReason] = useState("");
  const [issue, setIssue] = useState<BrowserIssue | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [menu, setMenu] = useState(false);
  const [nonces, setNonces] = useState<Record<string, number>>({});
  const abort = useRef<AbortController | null>(null);
  const fail = useCallback((error: unknown) => setIssue(browserIssue(error, serverName)), [serverName]);

  const list = useCallback(async () => {
    if (!currentServer) return null;
    const server = currentServer;
    const result = await browserRequest(server, { action: "list" }, abort.current?.signal);
    if (!isCurrentServer(server.id)) return null;
    const next = result.resources || [];
    setResources(next);
    setReason(result.capability?.available === false ? result.capability.reason || "Browser can't start on this server yet." : "");
    setLoaded(true);
    // Desktop apps run on a display of the agent's own; a server without them says so, which is fine.
    const shown = await listDisplays(server, abort.current?.signal).then((r) => r.displays, () => []);
    if (!isCurrentServer(server.id)) return null;
    setDisplays(shown);
    return { resources: next, available: result.capability?.available !== false };
  }, [currentServer, isCurrentServer]);

  useEffect(() => {
    setResources([]);
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

  // Server order, so a card keeps its place when control changes hands.
  const running = resources.filter((r) => r.state === "running");
  const nameOf = (resource: BrowserResource) => workerState.workers.find((w) => w.id === resource.agent)?.name;

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
  if (running.length === 0 && displays.length === 0) {
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
      <ScrollView contentContainerStyle={styles.list}>
        <View style={styles.inner}>
          {issue ? <InlineNotice tone={issue.tone} title={issue.title} detail={issue.detail} /> : null}
          {displays.map((display) => (
            <AgentDisplayCard key={`display:${display.id}`} server={currentServer} display={display}
              agentName={workerState.workers.find((w) => w.id === display.id)?.name}
              onStop={async (worker) => { await haltDisplay(currentServer, worker, abort.current?.signal); await list(); }}
              onResume={async (worker) => { await resumeDisplay(currentServer, worker, abort.current?.signal); await list(); }} />
          ))}
          {running.map((resource) => (
            <AgentBrowserCard key={`${resource.id}:${nonces[resource.id] ?? 0}`} server={currentServer} serverName={serverName}
              resource={resource} agentName={nameOf(resource)}
              onReconnect={() => setNonces((n) => ({ ...n, [resource.id]: (n[resource.id] ?? 0) + 1 }))} />
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
