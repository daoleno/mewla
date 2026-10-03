import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, AppState, Pressable, ScrollView, StyleSheet, Switch, Text, TextInput, View } from "react-native";
import { WebView } from "react-native-webview";
import { useAppTheme } from "../../constants/tokens";
import { useCurrentServer } from "../../store/currentServer";
import { browserRequest, BrowserViewer, type BrowserFrame, type BrowserResource } from "../../services/browser";
import { browserRendererHTML } from "./renderer";

export function BrowserScreen() {
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { colors } = useAppTheme();
  const [resources, setResources] = useState<BrowserResource[]>([]);
  const [selected, setSelected] = useState<BrowserResource | null>(null);
  const [reason, setReason] = useState("");
  const [status, setStatus] = useState("");
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [text, setText] = useState("");
  const [scroll, setScroll] = useState(false);
  const [busy, setBusy] = useState(false);
  const [tabs, setTabs] = useState<{ id: string; title: string; url: string }[]>([]);
  const viewer = useRef<BrowserViewer | null>(null);
  const web = useRef<WebView>(null);
  const epoch = useRef(0);
  const ready = useRef(false);
  const latest = useRef<BrowserFrame | null>(null);
  const abort = useRef<AbortController | null>(null);
  const disconnect = useCallback(() => {
    epoch.current++;
    viewer.current?.close();
    viewer.current = null;
    latest.current = null;
    ready.current = false;
    setSelected(null);
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
    setReason(result.capability?.reason || "");
  }, [currentServer, isCurrentServer]);
  useEffect(() => {
    disconnect();
    setResources([]);
    setReason("");
    setStatus("");
    const controller = new AbortController();
    abort.current = controller;
    void refresh().catch((error) => { if (!controller.signal.aborted) setStatus(error.message); });
    const subscription = AppState.addEventListener("change", (state) => {
      if (state !== "active") { abort.current?.abort(); disconnect(); }
      else { abort.current = new AbortController(); void refresh().catch(() => {}); }
    });
    return () => { abort.current?.abort(); subscription.remove(); disconnect(); };
  }, [currentServer?.id, disconnect, refresh]);
  const run = async (action: () => Promise<unknown>) => {
    const token = epoch.current;
    setBusy(true);
    try { await action(); } catch (error) {
      if (token === epoch.current) setStatus(error instanceof Error ? error.message : "Browser action failed");
    } finally { setBusy(false); }
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
    const token = epoch.current;
    const server = currentServer;
    const result = await browserRequest(server, { action: "start", id: resource.id }, abort.current?.signal).catch((error) => {
      if (token === epoch.current) setStatus(error.message);
      throw error;
    });
    if (token !== epoch.current || !isCurrentServer(server.id)) return;
    setSelected(result.resource!);
    setStatus("Connecting…");
    const active = () => token === epoch.current && isCurrentServer(server.id);
    const connection = new BrowserViewer(server, resource.id, {
      frame(frame) {
        if (!active()) return;
        if (!ready.current) { latest.current = frame; return; }
        web.current?.injectJavaScript(`window.zenFrame(${JSON.stringify(frame)});true;`);
      },
      status(value) { if (active()) setStatus(value); },
      resource(value) { if (active()) setSelected(value); },
    });
    viewer.current = connection;
    try { await connection.connect(); } catch (error) {
      if (active()) setStatus(error instanceof Error ? error.message : "Connection failed");
    }
  };
  const command = async (kind: string, extra = {}) => {
    const token = epoch.current;
    const result = await viewer.current?.command({ kind, ...extra });
    if (token !== epoch.current) return;
    if (kind === "tabs" && Array.isArray(result?.result)) setTabs(result.result);
    if (kind === "dialog_status") {
      const value = result?.result as { message?: string; type?: string } | undefined;
      Alert.alert("Page dialog", value?.message || JSON.stringify(value || {}), [
        { text: "Dismiss", onPress: () => { void run(() => command("dialog_dismiss")); } },
        { text: "Accept", onPress: () => { void run(() => command("dialog_accept", { text })); } },
        { text: "Cancel", style: "cancel" },
      ]);
    }
  };
  const button = (label: string, action: () => void, disabled = false) => (
    <Pressable accessibilityRole="button" accessibilityLabel={label} disabled={disabled}
      onPress={action} style={[styles.button, { backgroundColor: colors.surfaceSubtle, opacity: disabled ? 0.45 : 1 }]}>
      <Text style={{ color: colors.textPrimary }}>{label}</Text>
    </Pressable>
  );
  const inputStyle = [styles.input, { color: colors.textPrimary, backgroundColor: colors.inputBackground, borderColor: colors.border }];
  return (
    <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
      <Text style={[styles.caption, { color: colors.textSecondary }]}>{currentServer?.name || "Choose a server in Settings"} · Persistent Browser</Text>
      {status ? <Text accessibilityLiveRegion="polite" style={[styles.caption, { color: colors.textSecondary }]}>{status}</Text> : null}
      {!selected ? (
        <ScrollView contentContainerStyle={styles.list} keyboardShouldPersistTaps="handled">
          <Text style={{ color: colors.textSecondary }}>Open your browser, sign in, then let a later Agent task use it. Closing a viewer or task keeps this profile.</Text>
          <Text style={[styles.caption, { color: colors.textSecondary }]}>Take control pauses connected Agent browser tools. Release lets them request control again.</Text>
          {reason ? <Text style={{ color: colors.warning }}>{reason}</Text> : null}
          <View style={styles.row}>
            <TextInput accessibilityLabel="Browser name" placeholder="Personal, work, research…" placeholderTextColor={colors.textTertiary} value={name} onChangeText={setName} style={inputStyle} />
            {button("Create", () => void run(async () => { await manage("create", undefined, { name }); setName(""); }), busy || !name.trim() || !currentServer)}
          </View>
          {resources.map((resource) => (
            <View key={resource.id} style={[styles.card, { backgroundColor: colors.bgSurface, borderColor: colors.border }]}>
              <Text style={[styles.title, { color: colors.textPrimary }]}>{resource.name}</Text>
              <Text style={{ color: colors.textSecondary }}>{resource.state} · {resource.control}</Text>
              <View style={styles.row}>
                <Text style={{ color: colors.textPrimary, flex: 1 }}>Allow managed Agents</Text>
                <Switch accessibilityLabel={`Allow Agents for ${resource.name}`} value={resource.allow_agents} onValueChange={(allow_agents) => void run(() => manage("grant", resource, { allow_agents }))} />
              </View>
              <View style={styles.row}>
                {button("Open", () => void run(() => open(resource)), busy)}
                {button("Stop", () => void run(() => manage("stop", resource)), busy)}
                {button("Delete profile", () => Alert.alert("Delete browser profile?", "This removes saved sign-ins and browser data from this server.", [
                  { text: "Cancel", style: "cancel" },
                  { text: "Delete", style: "destructive", onPress: () => void run(() => manage("delete", resource)) },
                ]), busy)}
              </View>
              <Text style={[styles.caption, { color: colors.textTertiary }]}>Choose this Browser when creating a Codex or Claude session.</Text>
            </View>
          ))}
          {button("Refresh", () => void run(refresh), busy || !currentServer)}
        </ScrollView>
      ) : (
        <>
          <View style={styles.row}>
            {button("Profiles", () => { disconnect(); void run(refresh); })}
            <Text style={{ color: colors.textPrimary, flex: 1 }}>{selected.name} · {selected.control}</Text>
            {button("Reconnect", () => void run(() => open(selected)), busy)}
          </View>
          <View style={styles.row}>
            {button("Take control", () => void run(async () => { await viewer.current?.control(); setStatus("You control this browser · Managed browser tools paused"); }), busy)}
            {button("Release", () => void run(async () => { await viewer.current?.release(); setSelected((r) => r && ({ ...r, control: "idle" })); }), busy)}
            {button(scroll ? "Scroll ✓" : "Scroll", () => { setScroll(!scroll); web.current?.injectJavaScript(`window.zenScroll(${!scroll});true;`); })}
          </View>
          <View style={styles.row}>
            <TextInput accessibilityLabel="Website address" autoCapitalize="none" autoCorrect={false} placeholder="https://…" placeholderTextColor={colors.textTertiary} value={address} onChangeText={setAddress} style={inputStyle} onSubmitEditing={() => void run(() => command("navigate", { url: address }))} />
            {button("Go", () => void run(() => command("navigate", { url: address })), busy)}
            {button("+ Tab", () => void run(() => command("new_tab", { url: address })), busy)}
            {button("Tabs", () => void run(() => command("tabs")), busy)}
          </View>
          {tabs.length > 0 ? <ScrollView horizontal style={{ maxHeight: 50 }}>{tabs.map((tab) => <View key={tab.id}>{button(tab.title || tab.url, () => void run(async () => { await command("select_tab", { target: tab.id }); setTabs([]); }))}</View>)}</ScrollView> : null}
          <WebView ref={web} style={styles.viewport} source={{ html: browserRendererHTML }} originWhitelist={["about:blank"]}
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
                  void viewer.current?.command({ kind: "input", input: message.input }).catch((error) => setStatus(error.message));
                }
              } catch { /* Ignore malformed renderer messages. */ }
            }} />
          <View style={styles.row}>
            <TextInput accessibilityLabel="Text for focused host field" secureTextEntry autoCorrect={false} autoCapitalize="none" value={text} onChangeText={setText} placeholder="Type into focused field" placeholderTextColor={colors.textTertiary} style={inputStyle} />
            {button("Send", () => void run(async () => { const value = text; setText(""); await command("input", { input: { kind: "text", text: value } }); }), busy)}
            {button("Tab", () => void run(() => command("press", { text: "Tab" })), busy)}
            {button("Enter", () => void run(() => command("press", { text: "Enter" })), busy)}
            {button("⌫", () => void run(() => command("press", { text: "Backspace" })), busy)}
            {button("Esc", () => void run(() => command("press", { text: "Escape" })), busy)}
            {button("Dialog", () => void run(() => command("dialog_status")), busy)}
          </View>
          <Text style={[styles.caption, { color: colors.textTertiary }]}>Remote page view. System dialogs, file pickers and device passkeys are not available here.</Text>
        </>
      )}
    </View>
  );
}
const styles = StyleSheet.create({
  root: { flex: 1, padding: 12, gap: 8 },
  list: { gap: 16, paddingBottom: 24 },
  caption: { fontSize: 12, lineHeight: 17 },
  title: { fontSize: 18, fontWeight: "600" },
  row: { flexDirection: "row", alignItems: "center", gap: 6, flexWrap: "wrap" },
  card: { padding: 16, gap: 12, borderWidth: StyleSheet.hairlineWidth, borderRadius: 16 },
  button: { minHeight: 44, paddingHorizontal: 12, justifyContent: "center", borderRadius: 10 },
  input: { minHeight: 44, minWidth: 100, flex: 1, paddingHorizontal: 10, borderWidth: StyleSheet.hairlineWidth, borderRadius: 10 },
  viewport: { flex: 1, backgroundColor: "#171717" },
});
