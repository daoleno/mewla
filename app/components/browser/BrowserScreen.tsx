import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, AppState, Pressable, ScrollView, StyleSheet, TextInput, View } from "react-native";
import { Stack, useRouter } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { WebView } from "react-native-webview";
import { ContinuousCorners, Radii, Spacing, TouchTarget, useAppTheme } from "../../constants/tokens";
import { useCurrentServer } from "../../store/currentServer";
import { browserRequest, BrowserViewer, type BrowserFrame, type BrowserResource } from "../../services/browser";
import { getDefaultBrowserId, setDefaultBrowserId } from "../../services/browserDefault";
import {
  browserIssue,
  controlOffer,
  DEFAULT_BROWSER_NAME,
  pickDefaultBrowser,
  RECONNECT_DELAYS_MS,
  recoveryLabel,
  type BrowserIssue,
} from "../../services/browserPageModel";
import { ActionMenu, Button, EmptyState, IconButton, InlineNotice } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { browserRendererHTML } from "./renderer";

type ViewerPhase = "idle" | "connecting" | "live" | "reconnecting" | "lost";

/**
 * Entering Browser opens the user's default remote browser and asks for input
 * when nobody else holds it. Switching, new browsers, close, delete and details
 * live in the options menu. Leaving releases only this viewer; the browser and
 * its sign-ins stay on the current server.
 */
export function BrowserScreen() {
  const router = useRouter();
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { colors } = useAppTheme();
  const serverName = currentServer?.name || "this server";
  const [resources, setResources] = useState<BrowserResource[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [selected, setSelected] = useState<BrowserResource | null>(null);
  const [reason, setReason] = useState("");
  const [issue, setIssue] = useState<BrowserIssue | null>(null);
  const [phase, setPhase] = useState<ViewerPhase>("idle");
  const [mine, setMine] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [menu, setMenu] = useState(false);
  const [typing, setTyping] = useState(false);
  const [address, setAddress] = useState("");
  const [text, setText] = useState("");
  const [scroll, setScroll] = useState(false);
  const [tabs, setTabs] = useState<{ id: string; title: string; url: string }[]>([]);
  const viewer = useRef<BrowserViewer | null>(null);
  const web = useRef<WebView>(null);
  const epoch = useRef(0);
  const ready = useRef(false);
  const latest = useRef<BrowserFrame | null>(null);
  const abort = useRef<AbortController | null>(null);
  const current = useRef<BrowserResource | null>(null);
  const attempt = useRef(0);
  const retry = useRef<ReturnType<typeof setTimeout> | null>(null);
  const fail = useCallback((error: unknown) => setIssue(browserIssue(error, serverName)), [serverName]);
  const disconnect = useCallback(() => {
    epoch.current++;
    if (retry.current) clearTimeout(retry.current);
    retry.current = null;
    viewer.current?.close();
    viewer.current = null;
    latest.current = null;
    ready.current = false;
    current.current = null;
    setSelected(null);
    setPhase("idle");
    setMine(false);
    setTabs([]);
    setText("");
    setAddress("");
  }, []);
  const list = useCallback(async () => {
    if (!currentServer) return null;
    const token = epoch.current;
    const result = await browserRequest(currentServer, { action: "list" }, abort.current?.signal);
    if (epoch.current !== token || !isCurrentServer(currentServer.id)) return null;
    const next = result.resources || [];
    setResources(next);
    setReason(result.capability?.available === false ? result.capability.reason || "Browser can't start on this server yet." : "");
    setLoaded(true);
    return { resources: next, available: result.capability?.available !== false };
  }, [currentServer, isCurrentServer]);

  // Input is enabled only by the server-issued lease (see the lease handler).
  const acquire = useCallback(async () => {
    const connection = viewer.current;
    if (!connection) return;
    const token = epoch.current;
    await connection.control();
    if (token === epoch.current) setIssue(null);
  }, []);

  const connect = useCallback(async (resource: BrowserResource, reconnecting: boolean): Promise<void> => {
    if (!currentServer) return;
    const server = currentServer;
    viewer.current?.close();
    viewer.current = null;
    epoch.current++;
    const token = epoch.current;
    ready.current = false;
    latest.current = null;
    const started = await browserRequest(server, { action: "start", id: resource.id }, abort.current?.signal).catch((error) => {
      if (token === epoch.current) fail(error);
      throw error;
    });
    if (token !== epoch.current || !isCurrentServer(server.id)) return;
    const fresh = started.resource ?? resource;
    current.current = fresh;
    setSelected(fresh);
    setPhase(reconnecting ? "reconnecting" : "connecting");
    const active = () => token === epoch.current && isCurrentServer(server.id);
    let claimable = false;
    const connection = new BrowserViewer(server, resource.id, {
      frame(frame) {
        if (!active()) return;
        if (!ready.current) { latest.current = frame; return; }
        web.current?.injectJavaScript(`window.zenFrame(${JSON.stringify(frame)});true;`);
      },
      status() {},
      resource(value) {
        if (!active()) return;
        current.current = value;
        setSelected(value);
        // Claim input only from a fresh observation that nobody holds it;
        // never override a controller that appeared while disconnected.
        if (!claimable) return;
        claimable = false;
        if (controlOffer(value) === "acquire") void acquire().catch((error) => { if (active()) fail(error); });
      },
      lease(held) { if (active()) setMine(held); },
      connection(value) {
        if (!active()) return;
        if (value === "live") {
          attempt.current = 0;
          setPhase("live");
          setIssue(null);
          claimable = true;
          return;
        }
        setMine(false);
        const delay = RECONNECT_DELAYS_MS[attempt.current];
        if (delay === undefined) { setPhase("lost"); return; }
        attempt.current++;
        setPhase("reconnecting");
        retry.current = setTimeout(() => {
          retry.current = null;
          if (active()) void connect(resource, true).catch(() => { if (active()) setPhase("lost"); });
        }, delay);
      },
    });
    viewer.current = connection;
    try { await connection.connect(); } catch (error) {
      if (active()) { setPhase("lost"); fail(error); }
    }
  }, [acquire, currentServer, fail, isCurrentServer]);

  // Opening a browser is the user's choice to use it, including by Agents on
  // this server. A browser created before that default is granted once here.
  const open = useCallback(async (target: BrowserResource | null, name = DEFAULT_BROWSER_NAME) => {
    if (!currentServer) return;
    const server = currentServer;
    setIssue(null);
    attempt.current = 0;
    let resource = target;
    if (!resource) {
      const created = await browserRequest(server, { action: "create", name }, abort.current?.signal);
      resource = created.resource ?? null;
      if (!resource || !isCurrentServer(server.id)) return;
    }
    if (!resource.allow_agents) {
      const granted = await browserRequest(server, { action: "grant", id: resource.id, allow_agents: true }, abort.current?.signal);
      resource = granted.resource ?? resource;
    }
    if (!isCurrentServer(server.id)) return;
    await setDefaultBrowserId(server.id, resource.id);
    await connect(resource, false);
  }, [connect, currentServer, isCurrentServer]);

  const enter = useCallback(async () => {
    if (!currentServer) return;
    const server = currentServer;
    const listed = await list();
    if (!listed) return;
    const remembered = await getDefaultBrowserId(server.id);
    if (!isCurrentServer(server.id)) return;
    const target = pickDefaultBrowser(listed.resources, remembered);
    // A first browser is created only when the user taps Open browser; a closed
    // one can start only once the server is set up for it.
    if (!target || (target.state !== "running" && !listed.available)) return;
    await open(target);
  }, [currentServer, isCurrentServer, list, open]);

  useEffect(() => {
    disconnect();
    setResources([]);
    setLoaded(false);
    setReason("");
    setIssue(null);
    const controller = new AbortController();
    abort.current = controller;
    void enter().catch((error) => { if (!controller.signal.aborted) { setLoaded(true); fail(error); } });
    const subscription = AppState.addEventListener("change", (appState) => {
      if (appState !== "active") { abort.current?.abort(); disconnect(); }
      else { abort.current = new AbortController(); void enter().catch(() => {}); }
    });
    return () => { abort.current?.abort(); subscription.remove(); disconnect(); };
  }, [currentServer?.id, disconnect, enter]);

  const run = async (key: string, action: () => Promise<unknown>) => {
    const token = epoch.current;
    setBusy(key);
    try { await action(); } catch (error) {
      if (token === epoch.current) fail(error);
    } finally { setBusy(null); }
  };
  const command = async (kind: string, extra = {}) => {
    const token = epoch.current;
    const result = await viewer.current?.command({ kind, ...extra });
    if (token !== epoch.current) return;
    if (kind === "tabs" && Array.isArray(result?.result)) setTabs(result.result);
    if (kind === "dialog_status") {
      const value = result?.result as { message?: string } | undefined;
      Alert.alert("Page dialog", value?.message || "This page is showing a dialog.", [
        { text: "Dismiss", onPress: () => { void run("dialog", () => command("dialog_dismiss")); } },
        { text: "Accept", onPress: () => { void run("dialog", () => command("dialog_accept", { text })); } },
        { text: "Cancel", style: "cancel" },
      ]);
    }
  };
  const manage = async (action: "stop" | "delete", resource: BrowserResource) => {
    if (!currentServer) return;
    const server = currentServer;
    if (current.current?.id === resource.id) disconnect();
    await browserRequest(server, { action, id: resource.id }, abort.current?.signal);
    if (isCurrentServer(server.id)) await list();
  };
  const recover = (target: BrowserIssue) => {
    const resource = current.current;
    switch (target.recovery) {
      case "retry": void run("enter", enter); break;
      case "reconnect": if (resource) void run("open", () => open(resource)); else void run("enter", enter); break;
      case "take_control": if (phase === "live") void run("control", acquire); else void run("enter", enter); break;
      case "settings": router.push("/settings"); break;
      case "restart": if (resource) void run("stop", () => manage("stop", resource)); break;
    }
  };
  const confirmDelete = (resource: BrowserResource) => Alert.alert(
    `Delete “${resource.name}”?`,
    `This removes its saved sign-ins and browsing data from ${serverName}.`,
    [
      { text: "Cancel", style: "cancel" },
      { text: "Delete", style: "destructive", onPress: () => void run("delete", () => manage("delete", resource)) },
    ],
  );
  const showDetails = () => Alert.alert("Connection details", [
    issue ? issue.diagnostic : "No recent errors.",
    reason ? `Server: ${reason}` : "",
    selected ? `State: ${selected.state}, input: ${selected.control}${mine ? " (this phone)" : ""}` : "",
  ].filter(Boolean).join("\n\n"));

  const menuItems: ActionMenuItem[] = [
    ...resources.filter((r) => r.id !== selected?.id).map((resource): ActionMenuItem => ({
      key: `switch:${resource.id}`, label: `Switch to ${resource.name}`, icon: "swap-horizontal-outline",
      detail: resource.state === "running" ? "Open" : "Closed",
      // Closed browsers can't start until the server finishes setup.
      disabled: Boolean(reason) && resource.state !== "running",
      onPress: () => void run("open", () => open(resource)),
    })),
    ...(resources.length > 0 && resources.length < 8 ? [{
      key: "new", label: "New browser", icon: "add-circle-outline", detail: "Separate sign-ins", disabled: Boolean(reason),
      onPress: () => void run("open", () => open(null, `${DEFAULT_BROWSER_NAME} ${resources.length + 1}`)),
    } satisfies ActionMenuItem] : []),
    ...(selected && mine ? [{
      key: "release", label: "Stop controlling", icon: "hand-left-outline",
      onPress: () => void run("control", async () => { await viewer.current?.release(); setMine(false); }),
    } satisfies ActionMenuItem] : []),
    ...(selected && mine ? [{ key: "dialog", label: "Page dialog", icon: "chatbox-outline", onPress: () => void run("dialog", () => command("dialog_status")) } satisfies ActionMenuItem] : []),
    ...(selected ? [{ key: "stop", label: "Close browser", icon: "stop-circle-outline", detail: "Sign-ins are kept", onPress: () => void run("stop", () => manage("stop", selected)) } satisfies ActionMenuItem] : []),
    ...(selected ? [{ key: "delete", label: "Delete browser", icon: "trash-outline", destructive: true, onPress: () => confirmDelete(selected) } satisfies ActionMenuItem] : []),
    { key: "details", label: "Connection details", icon: "information-circle-outline", onPress: showDetails },
  ];
  const header = (
    <Stack.Screen options={{
      title: selected?.name ?? "Browser",
      headerRight: () => currentServer ? (
        <IconButton icon="ellipsis-horizontal" size={40} accessibilityLabel="Browser options" onPress={() => setMenu(true)} />
      ) : null,
    }} />
  );
  const sheet = <ActionMenu visible={menu} title={selected?.name ?? "Browser"} items={menuItems} onClose={() => setMenu(false)} />;
  const issueAction = issue ? recoveryLabel(issue.recovery) : null;
  const issueNotice = issue ? (
    <InlineNotice tone={issue.tone} title={issue.title} detail={issue.detail}
      action={issueAction ? { label: issueAction, onPress: () => recover(issue), disabled: busy !== null } : undefined} />
  ) : null;

  if (!currentServer) {
    return (
      <View style={[styles.root, styles.center, { backgroundColor: colors.bgPrimary }]}>
        <EmptyState icon="server-outline" title="No current server" detail="Choose one in Settings." action={{ label: "Open Settings", onPress: () => router.push("/settings") }} />
      </View>
    );
  }

  if (!selected) {
    const target = pickDefaultBrowser(resources, null);
    const setupNeeded = Boolean(reason) && !resources.some((r) => r.state === "running");
    return (
      <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
        {header}
        {sheet}
        {issueNotice ? <View style={styles.notice}>{issueNotice}</View> : null}
        <View style={[styles.root, styles.center]}>
          {!loaded || busy === "open" ? (
            <EmptyState busy title="Opening your browser…" />
          ) : setupNeeded ? (
            <EmptyState icon="construct-outline" title="Browser needs setup on this server" detail={reason}
              action={{ label: "Check again", icon: "refresh", onPress: () => void run("enter", enter), loading: busy === "enter" }} />
          ) : (
            // While a notice offers recovery, it is the screen's one action.
            <EmptyState icon="globe-outline" title="No browser yet"
              detail={`Sign-ins are kept on ${serverName}.`}
              action={issue ? undefined : { label: "Open browser", icon: "open-outline", onPress: () => void run("open", () => open(target)) }} />
          )}
        </View>
      </View>
    );
  }

  const offer = controlOffer(selected);
  const viewOnly = phase === "live" && !mine;
  const notice =
    phase === "lost" ? (
      <InlineNotice tone="warning" title="Lost connection to the browser" detail={`Still open on ${serverName}.`}
        action={{ label: "Reconnect", onPress: () => void run("open", () => open(selected)), disabled: busy !== null }} />
    ) : issueNotice ? issueNotice
    : phase === "reconnecting" ? <InlineNotice busy title="Reconnecting…" />
    : phase === "connecting" ? <InlineNotice busy title="Connecting…" />
    : viewOnly && offer === "takeover" ? (
      <InlineNotice tone="accent" icon={selected.control === "agent" ? "git-network-outline" : "phone-portrait-outline"}
        title={selected.control === "agent" ? "An Agent is using this browser" : "In use on another device"}
        detail={selected.control === "agent" ? "The Agent pauses while you're in control." : undefined}
        action={{ label: "Take over", onPress: () => void run("control", acquire), disabled: busy !== null }} />
    ) : viewOnly && offer === "acquire" ? (
      <InlineNotice title="Viewing only"
        action={{ label: "Use browser", onPress: () => void run("control", acquire), disabled: busy !== null }} />
    ) : null;
  const inputStyle = [styles.input, { color: colors.textPrimary, backgroundColor: colors.inputBackground, borderColor: colors.border }];
  return (
    <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
      {header}
      {sheet}
      {notice ? <View style={styles.notice}>{notice}</View> : null}
      {mine ? (
        <View style={styles.bar}>
          <TextInput accessibilityLabel="Website address" autoCapitalize="none" autoCorrect={false} keyboardType="url" returnKeyType="go"
            placeholder="Enter a website" placeholderTextColor={colors.textTertiary} value={address} onChangeText={setAddress}
            selectTextOnFocus style={inputStyle}
            onSubmitEditing={() => { if (address.trim()) void run("nav", () => command("navigate", { url: address.trim() })); }} />
          <IconButton icon="copy-outline" size={44} accessibilityLabel="Tabs" onPress={() => void run("tab", () => command("tabs"))} />
          <IconButton icon="keypad-outline" size={44} tone={typing ? "tinted" : "default"} accessibilityLabel={typing ? "Hide typing tools" : "Show typing tools"}
            accessibilityState={{ expanded: typing }} onPress={() => setTyping(!typing)} />
        </View>
      ) : null}
      {mine && tabs.length > 0 ? (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.chipRow} contentContainerStyle={styles.chips}>
          <Button label="New tab" icon="add" variant="outlined" size="sm" hitSlop={6} onPress={() => void run("tab", async () => { await command("new_tab", { url: "about:blank" }); setTabs([]); })} />
          {tabs.map((tab) => (
            <Button key={tab.id} label={tab.title || tab.url} variant="tinted" size="sm" hitSlop={6}
              onPress={() => void run("tab", async () => { await command("select_tab", { target: tab.id }); setTabs([]); })} />
          ))}
        </ScrollView>
      ) : null}
      <WebView ref={web} style={[styles.viewport, { backgroundColor: colors.bgElevated }]} source={RENDERER_SOURCE} originWhitelist={["about:blank"]}
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
            else if (message.type === "input" && message.input && mine) {
              void viewer.current?.command({ kind: "input", input: message.input }).catch(fail);
            }
          } catch { /* Ignore malformed renderer messages. */ }
        }} />
      {mine && typing ? (
        <View style={styles.tray}>
          <View style={styles.bar}>
            <TextInput accessibilityLabel="Text to type into the focused field" secureTextEntry autoCorrect={false} autoCapitalize="none"
              value={text} onChangeText={setText} placeholder="Type into the focused field" placeholderTextColor={colors.textTertiary} style={inputStyle} />
            <Button label="Send" variant="tinted" disabled={!text} onPress={() => void run("type", async () => { const value = text; setText(""); await command("input", { input: { kind: "text", text: value } }); })} />
          </View>
          <View style={styles.keys}>
            {([["Tab", "Tab"], ["Enter", "Enter"], ["Backspace", "⌫"], ["Escape", "Esc"]] as const).map(([key, label]) => (
              <Button key={key} label={label} accessibilityLabel={key === "Backspace" ? "Backspace" : `${key} key`} variant="outlined" style={styles.key}
                onPress={() => void run("key", () => command("press", { text: key }))} />
            ))}
            <Pressable accessibilityRole="switch" accessibilityState={{ checked: scroll }} accessibilityLabel="Drag to scroll the page"
              onPress={() => { setScroll(!scroll); web.current?.injectJavaScript(`window.zenScroll(${!scroll});true;`); }}
              style={[styles.scroll, { backgroundColor: scroll ? colors.surfaceSubtle : "transparent", borderColor: colors.border }]}>
              <Ionicons name="swap-vertical" size={18} color={colors.textSecondary} />
            </Pressable>
          </View>
        </View>
      ) : null}
    </View>
  );
}

const RENDERER_SOURCE = { html: browserRendererHTML };

const styles = StyleSheet.create({
  root: { flex: 1 },
  center: { justifyContent: "center", padding: Spacing.lg },
  notice: { paddingHorizontal: Spacing.md, paddingTop: Spacing.sm },
  bar: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm },
  chipRow: { flexGrow: 0 },
  chips: { gap: Spacing.sm, paddingHorizontal: Spacing.md, paddingBottom: Spacing.sm },
  tray: { paddingBottom: Spacing.sm },
  keys: { flexDirection: "row", gap: Spacing.sm, paddingHorizontal: Spacing.md },
  key: { flex: 1 },
  scroll: { width: TouchTarget, minHeight: TouchTarget, alignItems: "center", justifyContent: "center", borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.sm, ...ContinuousCorners },
  input: { minHeight: TouchTarget, flex: 1, paddingHorizontal: Spacing.md, borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.sm, ...ContinuousCorners },
  viewport: { flex: 1 },
});
