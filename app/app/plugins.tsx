import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, AppState, BackHandler, Linking, ScrollView, StyleSheet, Switch, View } from "react-native";
import { Stack, useRouter } from "expo-router";
import * as WebBrowser from "expo-web-browser";
import * as SecureStore from "expo-secure-store";
import * as Clipboard from "expo-clipboard";
import { KeyboardAvoidingView } from "react-native-keyboard-controller";
import { useCurrentServer } from "../store/currentServer";
import { useAppColors } from "../constants/tokens";
import { AppText, Button, ListRow, ListSection } from "../components/ui";
import { CustomServiceForm } from "../components/plugins/CustomServiceForm";
import { wsClient } from "../services/websocket";
import { accountStatus, type ConnectionRequest, type ConnectionResponse, type PluginAccount, type PluginIntegration } from "../services/connections";
import { finishPluginReturn, matchesPluginReturn, pendingConnectionKey, retainPluginReturn, PLUGIN_CALLBACK, pluginJobs, type ConnectPhase, type PendingConnection } from "../services/pluginOnboarding";


type Page = "catalog" | "service" | "accounts" | "permissions" | "advanced" | "custom";

export default function PluginsScreen() {
  const { currentServerId, servers, isCurrentServer } = useCurrentServer();
  const [deferredServerId, setDeferredServerId] = useState<string | null>(null);
  useEffect(() => { if (deferredServerId === currentServerId) setDeferredServerId(null); }, [currentServerId, deferredServerId]);
  const deferReturn = useCallback(async (url: string) => {
    const owner = await retainPluginReturn(servers.filter((s) => !isCurrentServer(s.id)).map((s) => s.id), url, SecureStore);
    if (owner) setDeferredServerId(owner);
  }, [servers, isCurrentServer]);
  useEffect(() => {
    const receive = (url: string) => { void deferReturn(url).catch(() => undefined); };
    void Linking.getInitialURL().then((url) => { if (url) receive(url); });
    const listener = Linking.addEventListener("url", ({ url }) => receive(url));
    return () => listener.remove();
  }, [deferReturn]);
  const deferredName = deferredServerId && deferredServerId !== currentServerId ? servers.find((s) => s.id === deferredServerId)?.name : undefined;
  return <PluginCatalog key={currentServerId ?? "none"} serverId={currentServerId} deferredName={deferredName} deferReturn={deferReturn} />;
}
function PluginCatalog({ serverId, deferredName, deferReturn }: { serverId: string | null; deferredName?: string; deferReturn: (url: string) => Promise<void> }) {
  const PENDING_KEY = pendingConnectionKey(serverId);
  const colors = useAppColors();
  const router = useRouter();
  const { isCurrentServer } = useCurrentServer();
  const [catalog, setCatalog] = useState<PluginIntegration[]>([]);
  const [accounts, setAccounts] = useState<PluginAccount[]>([]);
  const [selected, setSelected] = useState<PluginIntegration | null>(null);
  const [account, setAccount] = useState<PluginAccount | null>(null);
  const [page, setPage] = useState<Page>("catalog");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [phase, setPhase] = useState<ConnectPhase>("idle");
  const [flow, setFlow] = useState<PendingConnection | null>(null);
  const [writes, setWrites] = useState(false);
  const [another, setAnother] = useState(false);
  const alive = useRef(true);
  const mutation = useRef(false);
  const completion = useRef(false);
  const browserOpen = useRef(false);
  const starting = useRef(false);
  const attempt = useRef(0);
  const pending = useRef<PendingConnection | null>(null);
  const valid = useCallback(() => alive.current && !!serverId && isCurrentServer(serverId), [serverId, isCurrentServer]);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const remember = useCallback(async (value: PendingConnection | null) => {
    pending.current = value; setFlow(value);
    if (value) await SecureStore.setItemAsync(PENDING_KEY, JSON.stringify(value));
    else await SecureStore.deleteItemAsync(PENDING_KEY);
  }, [PENDING_KEY]);
  const apply = useCallback((response: ConnectionResponse) => {
    if (!valid()) return;
    if (response.catalog) setCatalog(response.catalog);
    if (response.accounts) setAccounts(response.accounts);
    if (response.account) {
      const next = response.account;
      setAccount(next);
      setAccounts((old) => [...old.filter((item) => item.id !== next.id), next]);
      setAnother(false);
    }
  }, [valid]);
  const send = useCallback(async (request: ConnectionRequest): Promise<ConnectionResponse | undefined> => {
    if (!serverId || !valid() || mutation.current) return;
    mutation.current = true; setBusy(true); setError("");
    try { const result = await wsClient.requestConnections(serverId, request); if (valid()) { apply(result); return result; } }
    catch (failure) { if (valid()) setError(failure instanceof Error ? failure.message : "Could not reach this server. Try again."); }
    finally { mutation.current = false; if (valid()) setBusy(false); }
  }, [apply, serverId, valid]);
  const settle = useCallback(async (result: ConnectionResponse) => {
    if (!valid()) return;
    apply(result);
    if (result.flow?.status === "connected" && result.account) {
      setPhase("connected"); setPage("service"); await remember(null);
    } else if (result.flow?.status === "failed" || result.flow?.status === "cancelled") {
      setPhase(result.flow.status); setError(result.flow.message ?? "Connection cancelled. No new account was connected."); await remember(null);
    }
  }, [apply, remember, valid]);
  const finish = useCallback(async (url: string) => {
    const active = pending.current;
    if (!active || !valid() || !matchesPluginReturn(url, active.flow)) return;
    if (completion.current) { await remember({ ...active, callback: url }); return; }
    completion.current = true; setPhase("verifying"); setError("");
    try {
      await remember({ ...active, callback: url });
      const response = await finishPluginReturn(active, serverId, url, (id, request) => wsClient.requestConnections(id, request));
      await settle(response);
    } catch (failure) {
      if (valid()) { setPhase("failed"); setError(failure instanceof Error ? failure.message : "Could not verify authorization. Try again."); }
    } finally { completion.current = false; }
  }, [remember, serverId, settle, valid]);
  const check = useCallback(async () => {
    const active = pending.current;
    if (!active || !valid() || completion.current) return;
    if (Date.parse(active.flow.expires) <= Date.now()) {
      setPhase("failed"); setError("This connection expired. Connect again."); await remember(null); return;
    }
    if (active.callback) { await finish(active.callback); return; }
    completion.current = true;
    try { await settle(await wsClient.requestConnections(active.serverId, { action: "connect_status", flow_id: active.flow.id })); }
    catch (failure) { if (valid()) { setPhase("failed"); setError(failure instanceof Error ? failure.message : "Connection interrupted. Try again."); } }
    finally {
      completion.current = false;
      if (pending.current?.callback && valid()) void finish(pending.current.callback);
    }
  }, [finish, remember, settle, valid]);
  useEffect(() => {
    void send({ action: "list" });
    void SecureStore.getItemAsync(PENDING_KEY).then(async (raw) => {
      if (!raw || !valid()) return;
      try {
        const stored = JSON.parse(raw) as PendingConnection;
        if (stored.serverId !== serverId || Date.parse(stored.flow.expires) <= Date.now()) return;
        pending.current = stored; setFlow(stored); setPhase(stored.flow.status === "confirm" ? "idle" : "waiting");
        const list = await wsClient.requestConnections(serverId!, { action: "list" });
        if (!valid()) return;
        apply(list); setSelected(list.catalog?.find((p) => p.id === stored.flow.integration) ?? null); setPage("service");
        const initial = await Linking.getInitialURL();
        if (initial && matchesPluginReturn(initial, stored.flow)) await finish(initial); else await check();
      } catch { if (valid()) { setError("The previous connection could not be restored. Connect again."); await remember(null); } }
    });
    const connected = (event: { serverId: string }) => { if (event.serverId === serverId) { void send({ action: "list" }); void check(); } };
    const subscription = AppState.addEventListener("change", (state) => { if (state === "active") void check(); });
    const links = Linking.addEventListener("url", (event) => void finish(event.url));
    wsClient.on("connected", connected);
    return () => { subscription.remove(); links.remove(); wsClient.off("connected", connected); };
  }, [apply, check, finish, remember, send, serverId, valid]);
  useEffect(() => {
    if (phase !== "waiting" || !flow) return;
    const timer = setInterval(() => { if (AppState.currentState === "active") void check(); }, 3000);
    return () => clearInterval(timer);
  }, [check, flow, phase]);
  const cancel = async () => {
    attempt.current++;
    const active = pending.current;
    if (active && valid()) {
      try {
        const response = await wsClient.requestConnections(active.serverId, { action: "connect_cancel", flow_id: active.flow.id });
        if (response.account) { await settle(response); return; }
      }
      catch { /* Pending flows expire on the original server; they cannot execute. */ }
    }
    if (valid()) { await remember(null); setPhase("cancelled"); setError("Connection cancelled. You can try again."); }
  };
  const openBrowser = async (active: PendingConnection) => {
    if (!active.flow.authorization_url || !valid() || browserOpen.current) return;
    browserOpen.current = true;
    setPhase("opening");
    try {
      setPhase("waiting");
      if (active.flow.user_code) { await WebBrowser.openBrowserAsync(active.flow.authorization_url); await check(); return; }
      const result = await WebBrowser.openAuthSessionAsync(active.flow.authorization_url, PLUGIN_CALLBACK);
      if (!valid()) {
        if (result.type === "success") await deferReturn(result.url);
        return;
      }
      if (pending.current?.flow.id !== active.flow.id) return;
      if (result.type === "success") {
        if (matchesPluginReturn(result.url, active.flow)) await finish(result.url);
        else if (result.url === PLUGIN_CALLBACK) await check();
      }
      else if (!completion.current) await cancel();
    } catch { if (valid()) { setPhase("failed"); setError("The browser could not open. Try again."); } }
    finally { browserOpen.current = false; }
  };
  const authorize = async (input: NonNullable<ConnectionRequest["input"]>) => {
    if (mutation.current || starting.current || !valid()) return;
    starting.current = true;
    try {
      if (pending.current) await cancel();
      const currentAttempt = ++attempt.current;
      setPhase("opening"); setError("");
      const result = await send({ action: "connect_start", input });
      if (!result?.flow || !valid()) { if (valid()) setPhase("failed"); return; }
      if (currentAttempt !== attempt.current) {
        await wsClient.requestConnections(serverId!, { action: "connect_cancel", flow_id: result.flow.id }).catch(() => undefined);
        return;
      }
      const active = { serverId: serverId!, flow: result.flow };
      await remember(active);
      // Device authorization asks for a code on GitHub. Let people see and
      // copy it before leaving Zen, instead of opening a page with no code.
      if (active.flow.user_code) setPhase("waiting");
      else await openBrowser(active);
    } catch (failure) {
      if (valid()) { setPhase("failed"); setError(failure instanceof Error ? failure.message : "Could not start this connection. Try again."); }
    } finally { starting.current = false; }
  };
  const previewGitHub = async () => {
    if (pending.current) await cancel();
    const result = await send({ action: "github_preview" });
    if (result?.flow && valid()) { await remember({ serverId: serverId!, flow: result.flow }); setPhase("idle"); }
  };
  const importGitHub = async () => {
    if (!pending.current || busy) return;
    setPhase("verifying");
    const result = await send({ action: "github_import", flow_id: pending.current.flow.id, input: { integration: "github", allow_writes: writes } });
    if (result) await settle(result); else setPhase("failed");
  };
  const choose = async (plugin: PluginIntegration) => {
    if (mutation.current || starting.current) return;
    setSelected(plugin); setPage("service"); setAccount(null); setAnother(false); setError(""); setPhase("idle"); setWrites(false);
    const existing = accounts.find((item) => item.integration === plugin.id && item.status !== "disconnected");
    if (existing) await send({ action: "get", id: existing.id });
  };
  const back = () => {
    if (["opening", "waiting", "verifying"].includes(phase)) {
      Alert.alert("Leave this connection?", "You can connect again later.", [{ text: "Keep connecting", style: "cancel" }, { text: "Cancel connection", onPress: () => { void cancel().then(() => { if (valid()) setPage("catalog"); }); } }]); return;
    }
    if (pending.current) {
      void cancel().then(() => {
        if (!valid()) return;
        setError(""); setPhase("idle"); setPage("catalog"); setSelected(null); setAccount(null);
      });
      return;
    }
    setError("");
    if (page === "catalog") { router.back(); return; }
    if (page === "permissions" || page === "advanced" || page === "accounts") setPage("service");
    else { setPage("catalog"); setSelected(null); setAccount(null); }
  };
  useEffect(() => { const listener = BackHandler.addEventListener("hardwareBackPress", () => { back(); return true; }); return () => listener.remove(); });
  const changeGroup = (group: "read" | "write", allowed: boolean) => {
    if (!account) return;
    const id = account.id;
    const commit = () => { if (valid()) void send({ action: "permissions", id, group, allowed }); };
    if (allowed && group === "write") Alert.alert("Allow changes when asked?", pluginJobs[account.integration]?.write + ". Brain still needs your instruction before taking action.", [{ text: "Cancel", style: "cancel" }, { text: "Allow changes", onPress: commit }]);
    else commit();
  };
  const activeAccounts = accounts.filter((a) => a.status !== "disconnected");
  const pluginAccounts = activeAccounts.filter((a) => a.integration === selected?.id);
  const job = selected ? pluginJobs[selected.id] : undefined;
  const connecting = phase === "opening" || phase === "waiting" || phase === "verifying";
  const linked = account && !another && account.status !== "disconnected";
  const custom = selected?.id === "mcp" || selected?.id === "openapi";
  const title = page === "catalog" ? "Plugins" : page === "custom" ? "Custom services" : page === "permissions" ? "Permissions" : page === "advanced" ? "Tools & activity" : page === "accounts" ? "Connected accounts" : selected?.name ?? "Plugins";
  return <KeyboardAvoidingView style={{ flex: 1, backgroundColor: colors.bgPrimary }} behavior="padding">
    <Stack.Screen options={{ title, headerLeft: () => <Button label="Back" accessibilityLabel="Back" icon="chevron-back" variant="plain" onPress={back} />, gestureEnabled: page === "catalog" }} />
    <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
      <AppText variant="title">{title}</AppText>
      {!serverId ? <AppText tone="secondary">Choose a current server in Settings to connect services.</AppText> : null}
      {error ? <View accessibilityRole="alert"><AppText style={{ color: colors.dangerText }}>{error}</AppText></View> : null}
      {deferredName ? <ListSection title="Return to the original server">
        <ListRow title={`Authorization is saved for ${deferredName}`} subtitle="Choose that server in Settings to finish connecting. This server has not received its authorization." numberOfLines={3} />
        <Button label="Open server settings" onPress={() => router.push("/settings")} />
      </ListSection> : null}
      {page === "catalog" && serverId ? <>
        <AppText tone="secondary">Bring your work into Brain. Choose a service and connect your account.</AppText>
        <ListSection title="Services">
          {catalog.filter((p) => !!pluginJobs[p.id]).map((plugin) => {
            const connected = activeAccounts.filter((a) => a.integration === plugin.id);
            return <ListRow key={plugin.id} title={plugin.name} subtitle={connected.length ? connected.map((a) => a.name).join(", ") : plugin.description} value={connected.some((a) => a.status === "connected" && a.enabled) ? "Connected" : undefined} accessory="chevron" onPress={() => void choose(plugin)} disabled={busy} />;
          })}
        </ListSection>
        {!catalog.length ? <Button label={busy ? "Loading services…" : "Try again"} loading={busy} onPress={() => void send({ action: "list" })} /> : null}
        <ListSection><ListRow title="Custom services" subtitle="Connect your own MCP server or API" accessory="chevron" onPress={() => setPage("custom")} /></ListSection>
      </> : null}
      {page === "custom" ? <>
        <AppText tone="secondary">Advanced connections for services you or your organization operate.</AppText>
        <ListSection>{catalog.filter((p) => !pluginJobs[p.id]).map((plugin) => <ListRow key={plugin.id} title={plugin.name} subtitle={plugin.description} accessory="chevron" onPress={() => void choose(plugin)} />)}</ListSection>
      </> : null}
      {page === "service" && selected ? <>
        <AppText tone="secondary">{selected.description}</AppText>
        {connecting ? <ListSection title={phase === "opening" ? "Opening authorization…" : phase === "verifying" ? "Verifying your account…" : `Waiting for ${selected.name}`}>
          <ListRow title={phase === "verifying" ? "Checking access on your server" : "Finish in your browser"} subtitle="Your account appears here automatically after authorization." />
          {flow?.flow.user_code ? <>
            <ListRow title={flow.flow.user_code} subtitle="Enter this code on GitHub. No code is entered in Zen." />
          </> : null}
          {phase === "waiting" && flow ? <Button label={flow.flow.user_code ? "Copy code and open GitHub" : "Open authorization"} onPress={() => void (async () => {
            if (flow.flow.user_code) await Clipboard.setStringAsync(flow.flow.user_code);
            await openBrowser(flow);
          })()} /> : null}
          <Button label="Cancel connection" variant="plain" onPress={() => void cancel()} />
        </ListSection> : linked ? <>
          <ListSection title={accountStatus(account)}>
            <ListRow title={account.name} subtitle={account.identity || "This service does not provide an account name."} numberOfLines={3} />
          </ListSection>
          <ListSection title="What you can do">
            {account.tools?.some((tool) => tool.allowed) ? <ListRow title={job?.read ?? "Use the tools you authorize"} subtitle={job?.example ?? "Review individual permissions in Tools & activity."} numberOfLines={3} /> : <ListRow title="Choose permissions to get started" subtitle="Decide what Brain may use with this account." accessory="chevron" onPress={() => setPage(custom ? "advanced" : "permissions")} />}
            {account.tools?.some((t) => t.group === "write" && t.allowed) && job ? <ListRow title={job.write} subtitle="Only when you ask Brain to take action." numberOfLines={3} /> : null}
          </ListSection>
          {account.status === "error" ? <Button label="Check connection" loading={busy} onPress={() => void send({ action: "refresh", id: account.id })} /> : null}
          {!account.enabled ? <Button label="Resume connection" loading={busy} onPress={() => void send({ action: "enable", id: account.id })} /> : null}
          {account.status === "authorization_required" ? <Button label={`Reconnect ${selected.name}`} onPress={() => { setAnother(true); setPhase("idle"); }} /> : null}
          <ListSection>
            {!custom ? <ListRow title="Permissions" subtitle="Choose what Brain can do" accessory="chevron" onPress={() => setPage("permissions")} /> : null}
            <ListRow title="Connected accounts" value={String(pluginAccounts.length)} accessory="chevron" onPress={() => setPage("accounts")} />
            <ListRow title="Tools & activity" subtitle="Advanced permissions and recent calls" accessory="chevron" onPress={() => setPage("advanced")} />
          </ListSection>
        </> : custom ? <CustomServiceForm key={selected.id} plugin={selected} busy={busy} send={send} authorize={authorize} /> : <>
          <ListSection title="Let Brain help with your work" footer="Credentials stay on this server. You choose the account and access in the service’s official authorization screen.">
            <ListRow title="Read and search" subtitle={job?.read} numberOfLines={3} />
            <ListRow title="Make changes when asked" subtitle={job?.write} numberOfLines={3} trailing={<Switch accessibilityLabel="Allow changes when asked" value={writes} onValueChange={setWrites} disabled={busy} />} />
          </ListSection>
          {flow?.flow.status === "confirm" ? <ListSection title="Use the account signed in on this server?" footer="Connect copies this account’s credential into Zen’s private store. Future sign-ins on the server won’t change this connection.">
            <ListRow title={flow.flow.identity ?? "GitHub account"} subtitle="GitHub · identity verified" />
            <Button label={`Connect ${flow.flow.identity ?? "this account"}`} loading={busy} onPress={() => void importGitHub()} />
            <Button label="Choose another account" variant="plain" onPress={() => void cancel()} />
          </ListSection> : <>
            <Button label={`${phase === "failed" || phase === "cancelled" ? "Try again with" : "Connect"} ${selected.name}`} variant="filled" loading={busy} onPress={() => void authorize({ integration: selected.id, allow_writes: writes })} />
            {selected.id === "github" ? <Button label="Use account signed in on server" variant="plain" disabled={busy} onPress={() => void previewGitHub()} /> : null}
          </>}
          {phase === "failed" && pending.current?.callback ? <Button label="Retry verification" onPress={() => void check()} /> : null}
        </>}
      </> : null}
      {page === "accounts" && selected ? <>
        <ListSection>{pluginAccounts.map((item) => <ListRow key={item.id} title={item.name} subtitle={item.identity} value={accountStatus(item)} accessory="chevron" onPress={() => { void send({ action: "get", id: item.id }).then(() => setPage("service")); }} />)}</ListSection>
        <Button label="Connect another account" icon="add" onPress={() => { setAnother(true); setPage("service"); setPhase("idle"); setError(""); }} />
        {account ? <Button label={account.credential_removal_pending ? "Retry credential removal" : `Disconnect ${account.name}`} variant="destructive" disabled={busy} onPress={() => Alert.alert("Disconnect this account?", "Future calls stop immediately. Zen removes its saved credential and revokes access where supported.", [{ text: "Cancel", style: "cancel" }, { text: "Disconnect", style: "destructive", onPress: () => { if (valid()) void send({ action: "disconnect", id: account.id }).then((response) => { if (response && valid()) { setAccount(null); setPage("service"); } }); } }])} /> : null}
      </> : null}
      {page === "permissions" && account ? <ListSection title={account.name} footer="Permission to use a tool never replaces your instruction to send a message or make a change.">
        {(["read", "write"] as const).map((group) => {
          const tools = account.tools?.filter((t) => t.group === group) ?? [];
          return <ListRow key={group} title={group === "read" ? "Read and search" : "Make changes when asked"} subtitle={!tools.length ? "Not included in this account’s authorization. Reconnect to request access." : group === "read" ? job?.read : job?.write} numberOfLines={3} trailing={<Switch accessibilityLabel={group === "read" ? "Read and search" : "Make changes when asked"} disabled={busy || !tools.length} value={!!tools.length && tools.every((t) => t.allowed)} onValueChange={(allowed) => changeGroup(group, allowed)} />} />;
        })}
        <Button label="Reconnect to change access" variant="plain" onPress={() => { setAnother(true); setPage("service"); setPhase("idle"); }} />
      </ListSection> : null}
      {page === "advanced" && account ? <>
        <ListSection title="Account controls">
          <ListRow title="Enabled" trailing={<Switch accessibilityLabel="Enable account" value={account.enabled} disabled={busy} onValueChange={(enabled) => void send({ action: enabled ? "enable" : "disable", id: account.id })} />} />
          <ListRow title="Check service and tools" subtitle={account.verified_at ? `Last verified ${new Date(account.verified_at).toLocaleString()}` : undefined} onPress={() => void send({ action: "refresh", id: account.id })} disabled={busy} />
        </ListSection>
        <ListSection title="Individual tools" footer="Changed tool definitions require a new permission. Custom tools are never enabled by remote annotations.">
          {account.tools?.map((tool) => <View key={tool.name}>
            <ListRow title={tool.name} subtitle={tool.description} numberOfLines={3} trailing={<Switch accessibilityLabel={`Allow ${tool.name}`} value={tool.allowed} disabled={busy} onValueChange={(allowed) => {
              const commit = () => { if (valid()) void send({ action: "policy", id: account.id, tool: tool.name, allowed }); };
              if (!allowed) commit(); else Alert.alert("Allow this tool?", `${tool.description}\n\nAvailable to Brain and Workers for this account.`, [{ text: "Cancel", style: "cancel" }, { text: "Allow", onPress: commit }]);
            }} />} /><ToolSchema schema={tool.input_schema} />
          </View>)}
        </ListSection>
        <ListSection title="Recent calls">{account.history.length ? account.history.map((event, i) => <ListRow key={`${event.at}-${i}`} title={event.tool} subtitle={event.message || new Date(event.at).toLocaleString()} value={event.status} numberOfLines={3} />) : <ListRow title="No calls yet" />}</ListSection>
      </> : null}
    </ScrollView>
  </KeyboardAvoidingView>;
}
function ToolSchema({ schema }: { schema?: Record<string, unknown> }) {
  const [expanded, setExpanded] = useState(false);
  return <View style={{ padding: 12 }}><Button label={expanded ? "Hide inputs" : "Inspect inputs"} variant="plain" onPress={() => setExpanded(!expanded)} />{expanded ? <AppText variant="caption" selectable>{JSON.stringify(schema, null, 2)}</AppText> : null}</View>;
}
const styles = StyleSheet.create({ content: { padding: 20, paddingBottom: 48, gap: 20 } });
