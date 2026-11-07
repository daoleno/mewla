import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, AppState, ScrollView, StyleSheet, View } from "react-native";
import { Stack, useRouter } from "expo-router";
import { ContentWidth, Spacing, useAppTheme } from "../../constants/tokens";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkers } from "../../store/workers";
import { usePhoneUse } from "../../store/phoneUse";
import { browserRequest, type BrowserResource } from "../../services/browser";
import { getDefaultBrowserId, setDefaultBrowserId } from "../../services/browserDefault";
import { browserIssue, DEFAULT_BROWSER_NAME, pickDefaultBrowser, type BrowserIssue } from "../../services/browserPageModel";
import { serverDisplayName } from "../../services/serverDisplayName";
import { ActionMenu, EmptyState, IconButton, InlineNotice } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { AgentBrowserCard } from "./AgentBrowserCard";
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
    return { resources: next, available: result.capability?.available !== false };
  }, [currentServer, isCurrentServer]);

  useEffect(() => {
    setResources([]);
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

  // Opening a browser by hand is for signing in: the person's own choice to use it, agents included.
  const open = useCallback(async (target: BrowserResource | null, name = DEFAULT_BROWSER_NAME) => {
    if (!currentServer) return;
    const server = currentServer;
    setIssue(null);
    let resource = target;
    if (!resource) {
      const created = await browserRequest(server, { action: "create", name }, abort.current?.signal);
      resource = created.resource ?? null;
      if (!resource) return;
    }
    if (!resource.allow_agents) {
      const granted = await browserRequest(server, { action: "grant", id: resource.id, allow_agents: true }, abort.current?.signal);
      resource = granted.resource ?? resource;
    }
    await browserRequest(server, { action: "start", id: resource.id }, abort.current?.signal);
    if (!isCurrentServer(server.id)) return;
    await setDefaultBrowserId(server.id, resource.id);
    await list();
  }, [currentServer, isCurrentServer, list]);

  const manage = async (action: "stop" | "delete", resource: BrowserResource) => {
    if (!currentServer) return;
    const server = currentServer;
    await browserRequest(server, { action, id: resource.id }, abort.current?.signal);
    if (isCurrentServer(server.id)) await list();
  };
  const confirmDelete = (resource: BrowserResource) => Alert.alert(
    `Delete “${resource.name}”?`,
    `This removes its saved sign-ins and browsing data from ${serverName}.`,
    [
      { text: "Cancel", style: "cancel" },
      { text: "Delete", style: "destructive", onPress: () => void run("delete", () => manage("delete", resource)) },
    ],
  );

  // Server order, so a card keeps its place when control changes hands.
  const running = resources.filter((r) => r.state === "running");
  const nameOf = (resource: BrowserResource) => workerState.workers.find((w) => w.id === resource.agent)?.name;

  const menuItems: ActionMenuItem[] = [
    ...(resources.length < 8 ? [{
      key: "new", label: "New browser", icon: "add-circle", detail: "Separate sign-ins", disabled: Boolean(reason) || busy !== null,
      onPress: () => void run("open", () => open(null, resources.length > 0 ? `${DEFAULT_BROWSER_NAME} ${resources.length + 1}` : DEFAULT_BROWSER_NAME)),
    } satisfies ActionMenuItem] : []),
    ...resources.filter((r) => r.state !== "running").map((resource): ActionMenuItem => ({
      key: `open:${resource.id}`, label: `Open ${resource.name}`, icon: "open-external", detail: "Closed", disabled: Boolean(reason) || busy !== null,
      onPress: () => void run("open", () => open(resource)),
    })),
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
  if (running.length === 0) {
    const setupNeeded = Boolean(reason);
    return (
      <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
        {header}
        {sheet}
        {issue ? <View style={styles.notice}><InlineNotice tone={issue.tone} title={issue.title} detail={issue.detail} /></View> : null}
        <View style={[styles.root, styles.center]}>
          {!loaded || busy === "open" ? (
            <EmptyState busy />
          ) : setupNeeded ? (
            <EmptyState icon="construct" title="Browser needs setup on this server" detail={reason}
              action={{ label: "Check again", icon: "refresh", onPress: () => void run("check", async () => { await list(); }), loading: busy === "check" }} />
          ) : (
            <EmptyState icon="desktop" title="No agent is using a screen"
              detail={`Sign-ins are kept on ${serverName}.`}
              action={{
                label: "Open browser", icon: "open-external", loading: busy === "open",
                onPress: () => void run("open", async () => {
                  const remembered = await getDefaultBrowserId(currentServer.id);
                  await open(pickDefaultBrowser(resources, remembered));
                }),
              }} />
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
          {running.map((resource) => (
            <AgentBrowserCard key={`${resource.id}:${nonces[resource.id] ?? 0}`} server={currentServer} serverName={serverName}
              resource={resource} agentName={nameOf(resource)}
              onReconnect={() => setNonces((n) => ({ ...n, [resource.id]: (n[resource.id] ?? 0) + 1 }))}
              onClose={(r) => manage("stop", r)} onDelete={confirmDelete} />
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
