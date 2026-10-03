import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, AppState, Pressable, ScrollView, StyleSheet, Switch, TextInput, View } from "react-native";
import { Stack, useRouter } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { WebView } from "react-native-webview";
import { ContinuousCorners, Radii, Spacing, TouchTarget, useAppTheme } from "../../constants/tokens";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkers } from "../../store/workers";
import { browserRequest, BrowserViewer, type BrowserFrame, type BrowserResource } from "../../services/browser";
import {
  agentAvailability,
  browserIssue,
  browserPresence,
  recoveryLabel,
  type BrowserIssue,
} from "../../services/browserPageModel";
import { AppText, Button, EmptyState, InlineNotice, ListSection, StatusPill } from "../ui";
import { ServerContextRow, type ServerConnection } from "../extensions/ServerContextRow";
import { browserRendererHTML } from "./renderer";

type ViewerPhase = "connecting" | "live" | "lost";

export function BrowserScreen() {
  const router = useRouter();
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { state } = useWorkers();
  const { colors } = useAppTheme();
  const serverName = currentServer?.name || "this server";
  const connection: ServerConnection = currentServer ? state.serverConnections[currentServer.id] ?? "offline" : "offline";
  const [resources, setResources] = useState<BrowserResource[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [selected, setSelected] = useState<BrowserResource | null>(null);
  const [reason, setReason] = useState("");
  const [issue, setIssue] = useState<BrowserIssue | null>(null);
  const [phase, setPhase] = useState<ViewerPhase>("connecting");
  const [mine, setMine] = useState(false);
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [text, setText] = useState("");
  const [scroll, setScroll] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [tabs, setTabs] = useState<{ id: string; title: string; url: string }[]>([]);
  const viewer = useRef<BrowserViewer | null>(null);
  const web = useRef<WebView>(null);
  const epoch = useRef(0);
  const ready = useRef(false);
  const latest = useRef<BrowserFrame | null>(null);
  const abort = useRef<AbortController | null>(null);
  const fail = useCallback((error: unknown) => setIssue(browserIssue(error, serverName)), [serverName]);
  const disconnect = useCallback(() => {
    epoch.current++;
    viewer.current?.close();
    viewer.current = null;
    latest.current = null;
    ready.current = false;
    setSelected(null);
    setMine(false);
    setTabs([]);
    setText("");
    setAddress("");
  }, []);
  const refresh = useCallback(async () => {
    if (!currentServer) return;
    const token = epoch.current;
    const result = await browserRequest(currentServer, { action: "list" }, abort.current?.signal);
    if (epoch.current !== token || !isCurrentServer(currentServer.id)) return;
    setResources(result.resources || []);
    setReason(result.capability?.available === false ? result.capability.reason || "Browser can’t start on this server yet." : "");
    setLoaded(true);
    setIssue(null);
  }, [currentServer, isCurrentServer]);
  useEffect(() => {
    disconnect();
    setResources([]);
    setLoaded(false);
    setReason("");
    setIssue(null);
    const controller = new AbortController();
    abort.current = controller;
    void refresh().catch((error) => { if (!controller.signal.aborted) { setLoaded(true); fail(error); } });
    const subscription = AppState.addEventListener("change", (appState) => {
      if (appState !== "active") { abort.current?.abort(); disconnect(); }
      else { abort.current = new AbortController(); void refresh().catch(() => {}); }
    });
    return () => { abort.current?.abort(); subscription.remove(); disconnect(); };
  }, [currentServer?.id, disconnect, refresh]);
  const run = async (key: string, action: () => Promise<unknown>) => {
    const token = epoch.current;
    setBusy(key);
    try { await action(); } catch (error) {
      if (token === epoch.current) fail(error);
    } finally { setBusy(null); }
  };
  const manage = async (action: string, resource?: BrowserResource, extra?: { name?: string; allow_agents?: boolean }) => {
    if (!currentServer) return;
    const token = epoch.current;
    const result = await browserRequest(currentServer, { action, id: resource?.id, ...extra }, abort.current?.signal);
    if (token !== epoch.current || !isCurrentServer(currentServer.id)) return;
    if (result.resource && selected?.id === result.resource.id) setSelected(result.resource);
    await refresh();
    return result;
  };
  const open = async (resource: BrowserResource) => {
    if (!currentServer) return;
    disconnect();
    setIssue(null);
    const token = epoch.current;
    const server = currentServer;
    const result = await browserRequest(server, { action: "start", id: resource.id }, abort.current?.signal).catch((error) => {
      if (token === epoch.current) fail(error);
      throw error;
    });
    if (token !== epoch.current || !isCurrentServer(server.id)) return;
    setSelected(result.resource!);
    setPhase("connecting");
    const active = () => token === epoch.current && isCurrentServer(server.id);
    const connection = new BrowserViewer(server, resource.id, {
      frame(frame) {
        if (!active()) return;
        if (!ready.current) { latest.current = frame; return; }
        web.current?.injectJavaScript(`window.zenFrame(${JSON.stringify(frame)});true;`);
      },
      // Status text is informational; liveness and command errors drive the UI.
      status() {},
      resource(value) { if (active()) setSelected(value); },
      connection(value) {
        if (!active()) return;
        setPhase(value);
        if (value === "lost") setMine(false);
      },
    });
    viewer.current = connection;
    try { await connection.connect(); } catch (error) {
      if (active()) { setPhase("lost"); fail(error); }
    }
  };
  const command = async (kind: string, extra = {}) => {
    const token = epoch.current;
    const result = await viewer.current?.command({ kind, ...extra });
    if (token !== epoch.current) return;
    if (kind === "tabs" && Array.isArray(result?.result)) setTabs(result.result);
    if (kind === "dialog_status") {
      const value = result?.result as { message?: string; type?: string } | undefined;
      Alert.alert("Page dialog", value?.message || "This page is showing a dialog.", [
        { text: "Dismiss", onPress: () => { void run("dialog", () => command("dialog_dismiss")); } },
        { text: "Accept", onPress: () => { void run("dialog", () => command("dialog_accept", { text })); } },
        { text: "Cancel", style: "cancel" },
      ]);
    }
  };
  const takeControl = () => run("control", async () => {
    const token = epoch.current;
    setMine(false);
    await viewer.current?.control();
    if (token === epoch.current) { setMine(true); setIssue(null); }
  });
  const releaseControl = () => run("control", async () => {
    setMine(false);
    await viewer.current?.release();
    setSelected((r) => r && ({ ...r, control: "idle" }));
  });
  const confirmDelete = (resource: BrowserResource) => Alert.alert(
    `Delete “${resource.name}”?`,
    `This removes its saved sign-ins and browsing data from ${serverName}. Agent tasks can no longer use it.`,
    [
      { text: "Cancel", style: "cancel" },
      { text: "Delete", style: "destructive", onPress: () => void run(`delete:${resource.id}`, () => manage("delete", resource)) },
    ],
  );
  const recover = (target: BrowserIssue) => {
    switch (target.recovery) {
      case "retry": void run("refresh", refresh); break;
      case "reconnect": if (selected) void run(`open:${selected.id}`, () => open(selected)); else void run("refresh", refresh); break;
      case "take_control": if (selected && phase === "live") void takeControl(); else void run("refresh", refresh); break;
      case "settings": router.push("/settings"); break;
      case "restart": {
        const resource = selected;
        if (resource) { disconnect(); void run(`stop:${resource.id}`, () => manage("stop", resource)); }
        else void run("refresh", refresh);
        break;
      }
    }
  };
  const issueCard = issue ? <IssueCard issue={issue} onRecover={() => recover(issue)} busy={busy !== null} /> : null;
  const inputStyle = [styles.input, { color: colors.textPrimary, backgroundColor: colors.inputBackground, borderColor: colors.border }];

  if (!currentServer) {
    return (
      <View style={[styles.root, styles.center, { backgroundColor: colors.bgPrimary }]}>
        <EmptyState icon="server-outline" title="Choose a server" detail="Your browser lives on a server. Choose one in Settings to sign in once and reuse it." action={{ label: "Open Settings", onPress: () => router.push("/settings") }} />
      </View>
    );
  }

  if (selected) {
    const controlling = mine && selected.control === "human";
    const presence = phase === "lost"
      ? { label: "Disconnected", tone: "warning" as const, detail: "The browser is still open on the server." }
      : phase === "connecting"
        ? { label: "Connecting", tone: "accent" as const, detail: "Loading the page view…" }
        : browserPresence(selected, controlling);
    const agents = agentAvailability(selected);
    return (
      <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
        <Stack.Screen options={{ title: selected.name }} />
        <View style={styles.viewerHeader}>
          <View style={styles.statusRow}>
            <View style={styles.statusText} accessible accessibilityLabel={`${presence.label}. ${presence.detail} ${agents.label}.`}>
              <StatusPill label={presence.label} tone={presence.tone} live={phase === "connecting" || selected.control === "quiescing"} />
              <AppText variant="caption" tone="secondary" numberOfLines={2}>{presence.detail}</AppText>
            </View>
            <Button label="All browsers" icon="albums-outline" variant="plain" size="sm" hitSlop={8}
              accessibilityHint="Closes this view. The browser keeps running."
              onPress={() => { disconnect(); setPhase("connecting"); void run("refresh", refresh); }} />
          </View>
          {issueCard}
          {phase === "lost" ? (
            <Button label="Reconnect" icon="refresh" variant="filled" block loading={busy === `open:${selected.id}`} onPress={() => void run(`open:${selected.id}`, () => open(selected))} />
          ) : controlling ? (
            <Button label="Release control" icon="hand-left-outline" variant="tinted" block loading={busy === "control"} disabled={busy !== null && busy !== "control"}
              accessibilityHint="Agent tasks can use the browser again." onPress={() => void releaseControl()} />
          ) : (
            <Button label="Take control" icon="hand-right-outline" variant="filled" block loading={busy === "control"} disabled={phase !== "live" || (busy !== null && busy !== "control")}
              accessibilityHint="Lets you tap, type and navigate. Agent tasks wait until you release control." onPress={() => void takeControl()} />
          )}
          <AppText variant="caption" tone="tertiary">{agents.label}. Closing this view keeps the browser and its sign-ins.</AppText>
        </View>
        {controlling ? (
          <View style={styles.toolbar}>
            <View style={styles.row}>
              <TextInput accessibilityLabel="Website address" autoCapitalize="none" autoCorrect={false} keyboardType="url" returnKeyType="go"
                placeholder="Enter a website" placeholderTextColor={colors.textTertiary} value={address} onChangeText={setAddress} style={inputStyle}
                onSubmitEditing={() => void run("nav", () => command("navigate", { url: address }))} />
              <Button label="Go" variant="tinted" loading={busy === "nav"} disabled={!address.trim()} onPress={() => void run("nav", () => command("navigate", { url: address }))} />
            </View>
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.chips} keyboardShouldPersistTaps="handled">
              <Button label="New tab" icon="add" variant="outlined" size="sm" hitSlop={6} onPress={() => void run("tab", () => command("new_tab", { url: address }))} />
              <Button label="Tabs" icon="copy-outline" variant="outlined" size="sm" hitSlop={6} onPress={() => void run("tab", () => command("tabs"))} />
              <Button label={scroll ? "Scrolling" : "Scroll"} icon="swap-vertical" variant={scroll ? "tinted" : "outlined"} size="sm" hitSlop={6}
                accessibilityState={{ selected: scroll }} accessibilityHint="Drag to scroll the page instead of tapping it."
                onPress={() => { setScroll(!scroll); web.current?.injectJavaScript(`window.zenScroll(${!scroll});true;`); }} />
              <Button label="Page dialog" icon="chatbox-outline" variant="outlined" size="sm" hitSlop={6} onPress={() => void run("dialog", () => command("dialog_status"))} />
            </ScrollView>
            {tabs.length > 0 ? (
              <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.chips}>
                {tabs.map((tab) => (
                  <Button key={tab.id} label={tab.title || tab.url} variant="tinted" size="sm" hitSlop={6}
                    onPress={() => void run("tab", async () => { await command("select_tab", { target: tab.id }); setTabs([]); })} />
                ))}
              </ScrollView>
            ) : null}
          </View>
        ) : null}
        <WebView ref={web} style={styles.viewport} source={{ html: browserRendererHTML }} originWhitelist={["about:blank"]}
          accessibilityLabel={`Page view of ${selected.name}`}
          onShouldStartLoadWithRequest={(request) => request.url === "about:blank"}
          javaScriptCanOpenWindowsAutomatically={false} setSupportMultipleWindows={false}
          allowFileAccess={false} domStorageEnabled={false} sharedCookiesEnabled={false}
          thirdPartyCookiesEnabled={false} cacheEnabled={false} incognito scrollEnabled={false}
          onMessage={(event) => {
            if (event.nativeEvent.data.length > 16384) return;
            try {
              const message = JSON.parse(event.nativeEvent.data);
              if (message.type === "ready") {
                ready.current = true;
                if (latest.current) { web.current?.injectJavaScript(`window.zenFrame(${JSON.stringify(latest.current)});true;`); latest.current = null; }
              } else if (message.type === "ack" && Number.isSafeInteger(message.seq)) viewer.current?.ack(message.seq);
              else if (message.type === "input" && message.input && selected.control === "human") {
                void viewer.current?.command({ kind: "input", input: message.input }).catch(fail);
              }
            } catch { /* Ignore malformed renderer messages. */ }
          }} />
        {controlling ? (
          <View style={styles.toolbar}>
            <View style={styles.row}>
              <TextInput accessibilityLabel="Text to type into the focused field" secureTextEntry autoCorrect={false} autoCapitalize="none"
                value={text} onChangeText={setText} placeholder="Type into the focused field" placeholderTextColor={colors.textTertiary} style={inputStyle} />
              <Button label="Send" variant="tinted" disabled={!text} onPress={() => void run("type", async () => { const value = text; setText(""); await command("input", { input: { kind: "text", text: value } }); })} />
            </View>
            <View style={styles.keys}>
              {([["Tab", "Tab"], ["Enter", "Enter"], ["Backspace", "⌫"], ["Escape", "Esc"]] as const).map(([key, label]) => (
                <Button key={key} label={label} accessibilityLabel={key === "Backspace" ? "Backspace" : `${key} key`} variant="outlined" style={styles.key}
                  onPress={() => void run("key", () => command("press", { text: key }))} />
              ))}
            </View>
            <AppText variant="micro" tone="tertiary">System dialogs, file pickers and passkeys aren’t available in this view.</AppText>
          </View>
        ) : (
          <AppText variant="caption" tone="tertiary" style={styles.hint}>
            {phase === "live" ? "Take control to tap, type or go to a site." : "The page appears here once connected."}
          </AppText>
        )}
      </View>
    );
  }

  const creating = busy === "create";
  return (
    <ScrollView style={{ backgroundColor: colors.bgPrimary }} contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
      <ServerContextRow name={serverName} connection={connection} />
      {issueCard}
      {reason ? (
        <InlineNotice tone="warning" icon="construct-outline" title={`Browser needs setup on ${serverName}`} detail={reason}
          action={{ label: "Check again", onPress: () => void run("refresh", refresh), disabled: busy !== null }} />
      ) : null}
      {!loaded ? (
        <EmptyState size="inline" busy title="Loading browsers…" />
      ) : resources.length === 0 && !issue ? (
        <EmptyState icon="globe-outline" title="Sign in once, reuse it later"
          detail={`Create a browser on ${serverName} and sign in to the sites you need. Later, Agent tasks can continue with those sign-ins.`} />
      ) : null}
      {resources.length > 0 ? (
        <ListSection title="Browsers" footer={`Sign-ins stay on ${serverName}, even after you close a browser.`}>
          {resources.map((resource, index) => (
            <BrowserCard key={resource.id} resource={resource} divider={index > 0} busy={busy}
              onOpen={() => void run(`open:${resource.id}`, () => open(resource))}
              onStop={() => void run(`stop:${resource.id}`, () => manage("stop", resource))}
              onDelete={() => confirmDelete(resource)}
              onAllowAgents={(allow_agents) => void run(`grant:${resource.id}`, () => manage("grant", resource, { allow_agents }))} />
          ))}
        </ListSection>
      ) : null}
      {loaded ? (
        <ListSection title={resources.length ? "Add another browser" : "Create a browser"} footer="Use one browser per set of accounts, like Personal or Work.">
          <View style={styles.createRow}>
            <TextInput accessibilityLabel="Browser name" placeholder="Name, like Personal" placeholderTextColor={colors.textTertiary}
              value={name} onChangeText={setName} returnKeyType="done" style={inputStyle}
              onSubmitEditing={() => { if (name.trim() && busy === null) void run("create", async () => { await manage("create", undefined, { name }); setName(""); }); }} />
            <Button label="Create" variant={resources.length ? "tinted" : "filled"} loading={creating} disabled={!name.trim() || (busy !== null && !creating)}
              onPress={() => void run("create", async () => { await manage("create", undefined, { name }); setName(""); })} />
          </View>
        </ListSection>
      ) : null}
    </ScrollView>
  );
}

function BrowserCard({ resource, divider, busy, onOpen, onStop, onDelete, onAllowAgents }: {
  resource: BrowserResource;
  divider: boolean;
  busy: string | null;
  onOpen(): void;
  onStop(): void;
  onDelete(): void;
  onAllowAgents(value: boolean): void;
}) {
  const { colors } = useAppTheme();
  const presence = browserPresence(resource);
  const agents = agentAvailability(resource);
  const running = resource.state === "running";
  const locked = busy !== null;
  return (
    <View style={[styles.card, divider && { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: colors.border }]}>
      <View style={styles.cardHead} accessible accessibilityLabel={`${resource.name}, ${presence.label}. ${presence.detail}`}>
        <View style={[styles.glyph, { backgroundColor: colors.surfaceSubtle }]}>
          <Ionicons name="globe-outline" size={22} color={colors.textSecondary} />
        </View>
        <View style={styles.flex}>
          <AppText variant="subtitle" numberOfLines={1}>{resource.name}</AppText>
          <AppText variant="caption" tone="secondary">{presence.detail}</AppText>
        </View>
        <StatusPill label={presence.label} tone={presence.tone} live={resource.control === "quiescing"} />
      </View>
      <Button label="Open browser" icon="open-outline" variant="filled" block
        loading={busy === `open:${resource.id}`} disabled={locked && busy !== `open:${resource.id}`} onPress={onOpen} />
      <View style={styles.agentRow}>
        <View style={styles.flex}>
          <AppText variant="label">{agents.label}</AppText>
          <AppText variant="caption" tone="tertiary">{agents.detail}</AppText>
        </View>
        <Switch accessibilityLabel={`Let Agent tasks use ${resource.name}`} value={resource.allow_agents} disabled={locked}
          onValueChange={onAllowAgents} />
      </View>
      <View style={styles.row}>
        {running ? (
          <Button label="Close" icon="stop-circle-outline" variant="plain" style={styles.flex} loading={busy === `stop:${resource.id}`} disabled={locked}
            accessibilityLabel={`Close ${resource.name}`} accessibilityHint="Stops the browser. Sign-ins are kept." onPress={onStop} />
        ) : null}
        <Button label="Delete" icon="trash-outline" variant="plain" style={styles.flex} loading={busy === `delete:${resource.id}`} disabled={locked}
          accessibilityLabel={`Delete ${resource.name}`} accessibilityHint="Asks before removing saved sign-ins." onPress={onDelete} />
      </View>
    </View>
  );
}

function IssueCard({ issue, onRecover, busy }: { issue: BrowserIssue; onRecover(): void; busy: boolean }) {
  const { colors } = useAppTheme();
  const [open, setOpen] = useState(false);
  const label = recoveryLabel(issue.recovery);
  return (
    <View style={styles.issue}>
      <InlineNotice tone={issue.tone} title={issue.title} detail={issue.detail}
        action={label ? { label, onPress: onRecover, disabled: busy } : undefined} />
      <Pressable accessibilityRole="button" accessibilityState={{ expanded: open }} accessibilityLabel={open ? "Hide details" : "Show details"}
        onPress={() => setOpen(!open)} style={styles.details} hitSlop={4}>
        <Ionicons name={open ? "chevron-down" : "chevron-forward"} size={14} color={colors.textTertiary} />
        <AppText variant="caption" tone="tertiary">Details</AppText>
      </Pressable>
      {open ? <AppText variant="mono" tone="tertiary" selectable style={styles.diagnostic}>{issue.diagnostic}</AppText> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  center: { justifyContent: "center", padding: Spacing.lg },
  content: { paddingHorizontal: Spacing.lg, paddingTop: Spacing.md, paddingBottom: 48, gap: Spacing.lg },
  flex: { flex: 1 },
  row: { flexDirection: "row", alignItems: "center", gap: Spacing.sm },
  createRow: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, padding: Spacing.md },
  card: { padding: Spacing.lg, gap: Spacing.md },
  cardHead: { flexDirection: "row", alignItems: "center", gap: Spacing.md },
  glyph: { width: 44, height: 44, borderRadius: Radii.sm, alignItems: "center", justifyContent: "center", ...ContinuousCorners },
  agentRow: { flexDirection: "row", alignItems: "center", gap: Spacing.md, minHeight: TouchTarget },
  viewerHeader: { paddingHorizontal: Spacing.lg, paddingTop: Spacing.sm, paddingBottom: Spacing.md, gap: Spacing.md },
  statusRow: { flexDirection: "row", alignItems: "center", gap: Spacing.sm },
  statusText: { flex: 1, gap: Spacing.xs },
  toolbar: { paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm, gap: Spacing.sm },
  chips: { gap: Spacing.sm, paddingVertical: 2 },
  keys: { flexDirection: "row", gap: Spacing.sm },
  key: { flex: 1 },
  hint: { paddingHorizontal: Spacing.lg, paddingVertical: Spacing.md, textAlign: "center" },
  input: { minHeight: TouchTarget, flex: 1, paddingHorizontal: Spacing.md, borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.sm, ...ContinuousCorners },
  issue: { gap: 2 },
  details: { flexDirection: "row", alignItems: "center", gap: 4, minHeight: TouchTarget, alignSelf: "flex-start", paddingHorizontal: Spacing.xs },
  diagnostic: { fontSize: 12, lineHeight: 17, paddingHorizontal: Spacing.xs },
  viewport: { flex: 1, backgroundColor: "#171717" },
});
