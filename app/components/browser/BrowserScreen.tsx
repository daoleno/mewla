import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, AppState, Pressable, ScrollView, StyleSheet, TextInput, View } from "react-native";
import { Stack, useRouter } from "expo-router";
import { ContinuousCorners, Radii, Spacing, TouchTarget, useAppTheme } from "../../constants/tokens";
import { useCurrentServer } from "../../store/currentServer";
import { browserRequest, BrowserViewer, type BrowserFrame, type BrowserInput, type BrowserResource } from "../../services/browser";
import { getDefaultBrowserId, setDefaultBrowserId } from "../../services/browserDefault";
import {
  browserIssue,
  DEFAULT_BROWSER_NAME,
  pickDefaultBrowser,
  RECONNECT_DELAYS_MS,
  recoveryLabel,
  type BrowserIssue,
} from "../../services/browserPageModel";
import { ActionMenu, AppText, Button, EmptyState, IconButton, InlineNotice } from "../ui";
import { useWorkers } from "../../store/workers";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { BrowserFrameHost, type BrowserFrameHostHandle } from "./BrowserFrameHost";
import { Icon } from "../icons/Icon";
import { serverDisplayName } from "../../services/serverDisplayName";

type ViewerPhase = "idle" | "connecting" | "live" | "reconnecting" | "lost";

/**
 * An agent's browser on the current server's virtual display. Opening it is
 * Watch: the picture is live and the person's taps and typing reach the page
 * without taking it from the agent, which is told what they did and decides
 * for itself. Take over holds the browser for the person; Stop ends the
 * agent's use. Switching, new browsers, close, delete and details live in the
 * options menu. Leaving releases only this viewer; the browser and its
 * sign-ins stay on the current server.
 */
export function BrowserScreen({ embedded = false }: { embedded?: boolean } = {}) {
  const router = useRouter();
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { colors } = useAppTheme();
  const { state: workerState } = useWorkers();
  const serverName = serverDisplayName(currentServer?.name);
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
  const web = useRef<BrowserFrameHostHandle>(null);
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
    const connection = new BrowserViewer(server, resource.id, {
      frame(frame) {
        if (!active()) return;
        if (!ready.current) { latest.current = frame; return; }
        web.current?.frame(frame);
      },
      status() {},
      resource(value) {
        if (!active()) return;
        current.current = value;
        setSelected(value);
      },
      lease(held) { if (active()) setMine(held); },
      connection(value) {
        if (!active()) return;
        if (value === "live") {
          attempt.current = 0;
          setPhase("live");
          setIssue(null);
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
  }, [currentServer, fail, isCurrentServer]);

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
  // The person's typing and keys: under the lease when held, otherwise a touch.
  const sendInput = async (input: BrowserInput) => {
    if (mine) await command("input", { input });
    else viewer.current?.touch(input);
  };
  const sendKey = async (key: string) => {
    if (mine) { await command("press", { text: key }); return; }
    const text = key === "Enter" ? "\r" : undefined;
    viewer.current?.touch({ kind: "keyDown", key, text });
    viewer.current?.touch({ kind: "keyUp", key });
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
      key: `switch:${resource.id}`, label: `Switch to ${resource.name}`, icon: "swap-horizontal",
      detail: resource.state === "running" ? "Open" : "Closed",
      // Closed browsers can't start until the server finishes setup.
      disabled: Boolean(reason) && resource.state !== "running",
      onPress: () => void run("open", () => open(resource)),
    })),
    ...(resources.length > 0 && resources.length < 8 ? [{
      key: "new", label: "New browser", icon: "add-circle", detail: "Separate sign-ins", disabled: Boolean(reason),
      onPress: () => void run("open", () => open(null, `${DEFAULT_BROWSER_NAME} ${resources.length + 1}`)),
    } satisfies ActionMenuItem] : []),
    ...(selected && mine ? [{ key: "dialog", label: "Page dialog", icon: "chatbox", onPress: () => void run("dialog", () => command("dialog_status")) } satisfies ActionMenuItem] : []),
    ...(selected ? [{ key: "stop", label: "Close browser", icon: "stop-circle", detail: "Sign-ins are kept", onPress: () => void run("stop", () => manage("stop", selected)) } satisfies ActionMenuItem] : []),
    ...(selected ? [{ key: "delete", label: "Delete browser", icon: "trash", destructive: true, onPress: () => confirmDelete(selected) } satisfies ActionMenuItem] : []),
    { key: "details", label: "Connection details", icon: "info", onPress: showDetails },
  ];
  const header = (
    <Stack.Screen options={{
      title: embedded ? "Screen" : selected?.name ?? "Browser",
      headerRight: () => currentServer ? (
        <IconButton icon="more-vertical" size={40} accessibilityLabel="Browser options" onPress={() => setMenu(true)} />
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
        <EmptyState icon="server" title="No current server" detail="Choose one in Settings." action={{ label: "Open Settings", onPress: () => router.push("/settings") }} />
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
            <EmptyState icon="construct" title="Browser needs setup on this server" detail={reason}
              action={{ label: "Check again", icon: "refresh", onPress: () => void run("enter", enter), loading: busy === "enter" }} />
          ) : (
            // While a notice offers recovery, it is the screen's one action.
            <EmptyState icon="browser" title="No browser yet"
              detail={`Sign-ins are kept on ${serverName}.`}
              action={issue ? undefined : { label: "Open browser", icon: "open-external", onPress: () => void run("open", () => open(target)) }} />
          )}
        </View>
      </View>
    );
  }

  const live = phase === "live";
  const agentWorker = selected.control === "agent" ? workerState.workers.find((w) => w.id === selected.agent) : undefined;
  const agentName = agentWorker?.name || "An agent";
  const holder = mine ? "you" : selected.control === "agent" ? "agent" : selected.control === "human" ? "device" : "none";
  const notice =
    phase === "lost" ? (
      <InlineNotice tone="warning" title="Lost connection to the browser" detail={`Still open on ${serverName}.`}
        action={{ label: "Reconnect", onPress: () => void run("open", () => open(selected)), disabled: busy !== null }} />
    ) : issueNotice ? issueNotice
    : phase === "reconnecting" ? <InlineNotice busy title="Reconnecting…" />
    : phase === "connecting" ? <InlineNotice busy title="Connecting…" />
    : null;
  // Watch, Take over and Stop: one bar, whatever holds the browser.
  const controls = live && !notice ? (
    <View style={styles.controls}>
      <View style={styles.controlsText}>
        <AppText variant="label" style={{ color: colors.textPrimary }}>
          {holder === "you" ? "You're in control" : holder === "agent" ? `${agentName} is using this browser` : holder === "device" ? "In use on another device" : "Watching"}
        </AppText>
      </View>
      <IconButton icon="keypad" size={40} tone={typing ? "tinted" : "default"} accessibilityLabel={typing ? "Hide typing tools" : "Show typing tools"}
        accessibilityState={{ expanded: typing }} onPress={() => setTyping(!typing)} />
      {holder === "you" ? (
        <Button label="Hand back" variant="tinted" size="sm" disabled={busy !== null}
          onPress={() => void run("control", async () => { await viewer.current?.release(); setMine(false); })} />
      ) : (
        <>
          <Button label="Take over" variant="tinted" size="sm" disabled={busy !== null || selected.control === "quiescing"}
            onPress={() => void run("control", acquire)} />
          {holder === "agent" ? (
            <Button label="Stop" variant="outlined" size="sm" disabled={busy !== null}
              onPress={() => void run("stop-agent", async () => { await viewer.current?.halt(); })} />
          ) : null}
        </>
      )}
    </View>
  ) : null;
  const inputStyle = [styles.input, { color: colors.textPrimary, backgroundColor: colors.inputBackground, borderColor: colors.border }];
  return (
    <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
      {header}
      {sheet}
      {notice ? <View style={styles.notice}>{notice}</View> : null}
      {controls}
      {mine ? (
        <View style={styles.bar}>
          <TextInput accessibilityLabel="Website address" autoCapitalize="none" autoCorrect={false} keyboardType="url" returnKeyType="go"
            placeholder="Enter a website" placeholderTextColor={colors.textTertiary} value={address} onChangeText={setAddress}
            selectTextOnFocus style={inputStyle}
            onSubmitEditing={() => { if (address.trim()) void run("nav", () => command("navigate", { url: address.trim() })); }} />
          <IconButton icon="copy" size={44} accessibilityLabel="Tabs" onPress={() => void run("tab", () => command("tabs"))} />
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
      <BrowserFrameHost ref={web} backgroundColor={colors.bgElevated} accessibilityLabel={`Page view of ${selected.name}`}
        onMessage={(data) => {
          if (data.length > 16384) return;
          try {
            const message = JSON.parse(data);
            if (message.type === "ready") {
              ready.current = true;
              if (latest.current) { web.current?.frame(latest.current); latest.current = null; }
            } else if (message.type === "ack" && Number.isSafeInteger(message.seq)) viewer.current?.ack(message.seq);
            else if (message.type === "input" && message.input) {
              // Holding the browser, input is a command under the lease; watching, it is a touch
              // the agent hears about.
              if (mine) void viewer.current?.command({ kind: "input", input: message.input }).catch(fail);
              else if (live) viewer.current?.touch(message.input);
            }
          } catch { /* Ignore malformed renderer messages. */ }
        }} />
      {live && typing ? (
        <View style={styles.tray}>
          <View style={styles.bar}>
            <TextInput accessibilityLabel="Text to type into the focused field" secureTextEntry autoCorrect={false} autoCapitalize="none"
              value={text} onChangeText={setText} placeholder="Type into the focused field" placeholderTextColor={colors.textTertiary} style={inputStyle} />
            <Button label="Send" variant="tinted" disabled={!text} onPress={() => void run("type", async () => { const value = text; setText(""); await sendInput({ kind: "text", text: value }); })} />
          </View>
          <View style={styles.keys}>
            {([["Tab", "Tab"], ["Enter", "Enter"], ["Backspace", "⌫"], ["Escape", "Esc"]] as const).map(([key, label]) => (
              <Button key={key} label={label} accessibilityLabel={key === "Backspace" ? "Backspace" : `${key} key`} variant="outlined" style={styles.key}
                onPress={() => void run("key", () => sendKey(key))} />
            ))}
            <Pressable accessibilityRole="switch" accessibilityState={{ checked: scroll }} accessibilityLabel="Drag to scroll the page"
              onPress={() => { setScroll(!scroll); web.current?.scroll(!scroll); }}
              style={[styles.scroll, { backgroundColor: scroll ? colors.surfaceSubtle : "transparent", borderColor: colors.border }]}>
              <Icon name="swap-vertical" size={18} color={colors.textSecondary} />
            </Pressable>
          </View>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  center: { justifyContent: "center", padding: Spacing.lg },
  notice: { paddingHorizontal: Spacing.md, paddingTop: Spacing.sm },
  controls: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, paddingHorizontal: Spacing.md, paddingVertical: Spacing.sm },
  controlsText: { flex: 1 },
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
