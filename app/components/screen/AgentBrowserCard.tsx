import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, AppState, Pressable, ScrollView, StyleSheet, TextInput, View } from "react-native";
import { ContinuousCorners, Radii, Spacing, TouchTarget, useAppTheme } from "../../constants/tokens";
import { BrowserViewer, type BrowserFrame, type BrowserInput, type BrowserResource } from "../../services/browser";
import { browserIssue, RECONNECT_DELAYS_MS, recoveryLabel, type BrowserIssue } from "../../services/browserPageModel";
import type { StoredServer } from "../../services/storage";
import { ActionMenu, AppText, Button, IconButton, InlineNotice } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import { Icon } from "../icons/Icon";
import { BrowserFrameHost, type BrowserFrameHostHandle } from "../browser/BrowserFrameHost";

type ViewerPhase = "connecting" | "live" | "reconnecting" | "lost";

export interface AgentBrowserCardProps {
  server: StoredServer;
  serverName: string;
  resource: BrowserResource;
  /** The Worker holding it, by name, when an agent does. */
  agentName?: string;
  /** The viewer gave up: open it again. */
  onReconnect(): void;
}

/**
 * One agent's browser on the virtual screen, with its own live view. Showing
 * it is Watch: the person's taps and typing reach the page without taking it
 * from the agent, which is told what they did and decides for itself. Take
 * over holds the browser for the person; Stop ends the agent's use. Leaving
 * releases only this viewer; the window and the computer's sign-ins stay.
 */
export function AgentBrowserCard({ server, serverName, resource, agentName, onReconnect }: AgentBrowserCardProps) {
  const { colors } = useAppTheme();
  const [live, setLive] = useState<BrowserResource>(resource);
  const [phase, setPhase] = useState<ViewerPhase>("connecting");
  const [mine, setMine] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [issue, setIssue] = useState<BrowserIssue | null>(null);
  const [menu, setMenu] = useState(false);
  const [typing, setTyping] = useState(false);
  const [address, setAddress] = useState("");
  const [text, setText] = useState("");
  const [scroll, setScroll] = useState(false);
  const [tabs, setTabs] = useState<{ id: string; title: string; url: string }[]>([]);
  const viewer = useRef<BrowserViewer | null>(null);
  const web = useRef<BrowserFrameHostHandle>(null);
  const ready = useRef(false);
  const latest = useRef<BrowserFrame | null>(null);
  const attempt = useRef(0);
  const retry = useRef<ReturnType<typeof setTimeout> | null>(null);
  const epoch = useRef(0);
  const current = useRef(resource);
  // The connection belongs to the computer's id: a refreshed copy of the same
  // server must not reconnect every viewer.
  const serverRef = useRef(server);
  serverRef.current = server;
  const mineRef = useRef(false);
  mineRef.current = mine;

  // The list reads again every few seconds; its newer word about who holds the
  // browser is taken unless this viewer has fresher news over its own socket.
  useEffect(() => { setLive((previous) => (previous.id === resource.id ? resource : previous)); }, [resource]);

  const fail = useCallback((error: unknown) => setIssue(browserIssue(error, serverName)), [serverName]);

  useEffect(() => {
    const token = ++epoch.current;
    const active = () => token === epoch.current;
    const connect = async (reconnecting: boolean): Promise<void> => {
      viewer.current?.close();
      latest.current = null;
      setPhase(reconnecting ? "reconnecting" : "connecting");
      const connection = new BrowserViewer(serverRef.current, current.current.id, {
        frame(frame) {
          if (!active()) return;
          if (!ready.current) { latest.current = frame; return; }
          web.current?.frame(frame);
        },
        status() {},
        resource(value) { if (active()) setLive(value); },
        lease(held) { if (active()) setMine(held); },
        connection(value) {
          if (!active()) return;
          if (value === "live") { attempt.current = 0; setPhase("live"); setIssue(null); return; }
          setMine(false);
          const delay = RECONNECT_DELAYS_MS[attempt.current];
          if (delay === undefined) { setPhase("lost"); return; }
          attempt.current++;
          setPhase("reconnecting");
          retry.current = setTimeout(() => {
            retry.current = null;
            if (active()) void connect(true).catch(() => { if (active()) setPhase("lost"); });
          }, delay);
        },
      });
      viewer.current = connection;
      try { await connection.connect(); } catch (error) { if (active()) { setPhase("lost"); fail(error); } }
    };
    void connect(false);
    // A phone that leaves the app gives its viewers up; coming back reconnects them.
    const subscription = AppState.addEventListener("change", (state) => {
      if (state !== "active") { viewer.current?.close(); viewer.current = null; setMine(false); setPhase("lost"); }
      else if (!viewer.current) { attempt.current = 0; void connect(true).catch(() => { if (active()) setPhase("lost"); }); }
    });
    return () => {
      epoch.current++;
      subscription.remove();
      if (retry.current) clearTimeout(retry.current);
      retry.current = null;
      viewer.current?.close();
      viewer.current = null;
    };
  }, [server.id, resource.id, fail]);

  const run = async (key: string, action: () => Promise<unknown>) => {
    setBusy(key);
    try { await action(); } catch (error) { fail(error); } finally { setBusy(null); }
  };
  const acquire = async () => { await viewer.current?.control(); setIssue(null); };
  const command = async (kind: string, extra = {}) => {
    const result = await viewer.current?.command({ kind, ...extra });
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
    viewer.current?.touch({ kind: "keyDown", key, text: key === "Enter" ? "\r" : undefined });
    viewer.current?.touch({ kind: "keyUp", key });
  };
  const recover = (target: BrowserIssue) => {
    if (target.recovery === "take_control" && phase === "live") void run("control", acquire);
    else onReconnect();
  };

  const liveNow = phase === "live";
  const holder = mine ? "you" : live.control === "agent" ? "agent" : live.control === "human" ? "device" : "none";
  // The card stays the agent's while someone else holds it, so the list reads the same before and after a take over.
  const title = agentName || (holder === "agent" ? "An agent" : live.name);
  const subtitle = holder === "agent" ? live.name : holder === "you" ? `${live.name} · You are in control` : holder === "device" ? `${live.name} · In use on another device` : `${live.name} · Not in use`;
  const issueAction = issue ? recoveryLabel(issue.recovery) : null;
  const notice =
    phase === "lost" ? (
      <InlineNotice tone="warning" title="Lost connection to the browser" detail={`Still open on ${serverName}.`}
        action={{ label: "Reconnect", onPress: onReconnect, disabled: busy !== null }} />
    ) : issue ? (
      <InlineNotice tone={issue.tone} title={issue.title} detail={issue.detail}
        action={issueAction ? { label: issueAction, onPress: () => recover(issue), disabled: busy !== null } : undefined} />
    ) : phase === "reconnecting" ? <InlineNotice busy title="Reconnecting…" />
    : phase === "connecting" ? <InlineNotice busy title="Connecting…" />
    : null;

  const menuItems: ActionMenuItem[] = [
    ...(mine ? [{ key: "dialog", label: "Page dialog", icon: "chatbox", onPress: () => void run("dialog", () => command("dialog_status")) } satisfies ActionMenuItem] : []),
  ];
  const inputStyle = [styles.input, { color: colors.textPrimary, backgroundColor: colors.inputBackground, borderColor: colors.border }];

  return (
    <View style={[styles.card, { backgroundColor: colors.bgSurface, borderColor: colors.borderSubtle }]}>
      <ActionMenu visible={menu} title={live.name} items={menuItems} onClose={() => setMenu(false)} />
      <View style={styles.header}>
        <View style={[styles.dot, { backgroundColor: holder === "agent" ? colors.accent : holder === "you" ? colors.seal : colors.textTertiary }]} />
        <View style={styles.headerText}>
          <AppText variant="label" numberOfLines={1} style={{ color: colors.textPrimary }}>{title}</AppText>
          <AppText variant="caption" numberOfLines={1} style={{ color: colors.textSecondary }}>{subtitle}</AppText>
        </View>
        {menuItems.length > 0 ? <IconButton icon="more-vertical" size={36} accessibilityLabel={`Options for ${live.name}`} onPress={() => setMenu(true)} /> : null}
      </View>
      {notice ? <View style={styles.notice}>{notice}</View> : null}
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
      <View style={[styles.view, { backgroundColor: colors.bgElevated }]}>
        <BrowserFrameHost ref={web} backgroundColor={colors.bgElevated} accessibilityLabel={`Page view of ${live.name}`}
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
                if (mineRef.current) void viewer.current?.command({ kind: "input", input: message.input }).catch(fail);
                else if (viewer.current) viewer.current.touch(message.input);
              }
            } catch { /* Ignore malformed renderer messages. */ }
          }} />
      </View>
      {liveNow ? (
        <View style={styles.controls}>
          <IconButton icon="keypad" size={40} tone={typing ? "tinted" : "default"} accessibilityLabel={typing ? "Hide typing tools" : "Show typing tools"}
            accessibilityState={{ expanded: typing }} onPress={() => setTyping(!typing)} />
          <View style={styles.spacer} />
          {holder === "you" ? (
            <Button label="Hand back" variant="tinted" size="sm" disabled={busy !== null}
              onPress={() => void run("control", async () => { await viewer.current?.release(); setMine(false); setLive((l) => (l.control === "human" ? { ...l, control: "idle" } : l)); })} />
          ) : (
            <>
              <Button label="Take over" variant="tinted" size="sm" disabled={busy !== null || live.control === "quiescing"}
                onPress={() => void run("control", acquire)} />
              {holder === "agent" ? (
                <Button label="Stop" variant="outlined" size="sm" accessibilityLabel={`Stop ${title}`} disabled={busy !== null}
                  onPress={() => void run("stop-agent", async () => { await viewer.current?.halt(); })} />
              ) : null}
            </>
          )}
        </View>
      ) : null}
      {liveNow && typing ? (
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
  card: { borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.md, overflow: "hidden", ...ContinuousCorners },
  header: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, paddingLeft: Spacing.md, paddingRight: Spacing.xs, paddingVertical: Spacing.xs },
  headerText: { flex: 1 },
  dot: { width: 8, height: 8, borderRadius: 4 },
  notice: { paddingHorizontal: Spacing.sm, paddingBottom: Spacing.sm },
  view: { width: "100%", aspectRatio: 1280 / 800 },
  controls: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, paddingHorizontal: Spacing.sm, paddingVertical: Spacing.xs },
  spacer: { flex: 1 },
  bar: { flexDirection: "row", alignItems: "center", gap: Spacing.sm, paddingHorizontal: Spacing.sm, paddingVertical: Spacing.xs },
  chipRow: { flexGrow: 0 },
  chips: { gap: Spacing.sm, paddingHorizontal: Spacing.sm, paddingBottom: Spacing.sm },
  tray: { paddingBottom: Spacing.sm },
  keys: { flexDirection: "row", gap: Spacing.sm, paddingHorizontal: Spacing.sm },
  key: { flex: 1 },
  scroll: { width: TouchTarget, minHeight: TouchTarget, alignItems: "center", justifyContent: "center", borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.sm, ...ContinuousCorners },
  input: { minHeight: TouchTarget, flex: 1, paddingHorizontal: Spacing.md, borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.sm, ...ContinuousCorners },
});
