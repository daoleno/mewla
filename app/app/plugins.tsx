import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, AppState, BackHandler, Linking, StyleSheet, View } from "react-native";
import { Stack, useRouter } from "expo-router";
import { openPluginAuthorization } from "../services/pluginBrowser";
import * as SecureStore from "expo-secure-store";
import * as Clipboard from "expo-clipboard";
import { KeyboardAwareScrollView } from "react-native-keyboard-controller";
import { useCurrentServer } from "../store/currentServer";
import { Spacing, useAppColors } from "../constants/tokens";
import { Button, InlineNotice } from "../components/ui";
import { CustomServiceForm } from "../components/plugins/CustomServiceForm";
import { AccountsView, CatalogView, ConnectOfferView, ConnectProgressCard, CustomCatalogView, LinkedAccountView, NoServerState, PermissionsView, ServiceHeader, ToolsView } from "../components/plugins/PluginConnectionViews";
import { ServerContextRow, type ServerConnection } from "../components/extensions/ServerContextRow";
import { useWorkers } from "../store/workers";
import { isConnecting, pluginCatalogSections, type AccountRecoveryAction } from "../services/pluginConnectionsModel";
import { wsClient } from "../services/websocket";
import { type ConnectionRequest, type ConnectionResponse, type PluginAccount, type PluginIntegration } from "../services/connections";
import { finishPluginReturn, matchesPluginReturn, pendingConnectionKey, retainPluginReturn, PLUGIN_CALLBACK, pluginJobs, type ConnectPhase, type PendingConnection } from "../services/pluginOnboarding";


type Page = "catalog" | "service" | "accounts" | "permissions" | "advanced" | "custom";

export default function PluginsScreen() {
  const { currentServerId, currentServer, servers, isCurrentServer } = useCurrentServer();
  const { state } = useWorkers();
  const connection: ServerConnection = currentServerId ? state.serverConnections[currentServerId] ?? "offline" : "offline";
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
  return <PluginCatalog key={currentServerId ?? "none"} serverId={currentServerId} serverName={currentServer?.name ?? "this server"} connection={connection} deferredName={deferredName} deferReturn={deferReturn} />;
}
function PluginCatalog({ serverId, serverName, connection, deferredName, deferReturn }: { serverId: string | null; serverName: string; connection: ServerConnection; deferredName?: string; deferReturn: (url: string) => Promise<void> }) {
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
      const result = await openPluginAuthorization(active.flow.authorization_url, !!active.flow.user_code);
      if (result.type === "external") { await check(); return; }
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
  const choose = async (plugin: PluginIntegration, chosen?: PluginAccount) => {
    if (mutation.current || starting.current) return;
    setSelected(plugin); setPage("service"); setAccount(null); setAnother(false); setError(""); setPhase("idle"); setWrites(false);
    const existing = chosen ?? accounts.find((item) => item.integration === plugin.id && item.status !== "disconnected");
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
  const disconnect = (target: PluginAccount) => Alert.alert("Disconnect this account?", "Future calls stop immediately. Zen removes its saved credential and revokes access where supported.", [{ text: "Cancel", style: "cancel" }, { text: "Disconnect", style: "destructive", onPress: () => { if (valid()) void send({ action: "disconnect", id: target.id }).then((response) => { if (response && valid()) { setAccount(null); setPage("service"); } }); } }]);
  const toggleTool = (target: PluginAccount, tool: NonNullable<PluginAccount["tools"]>[number], allowed: boolean) => {
    const commit = () => { if (valid()) void send({ action: "policy", id: target.id, tool: tool.name, allowed }); };
    if (!allowed) commit(); else Alert.alert("Allow this tool?", `${tool.description}\n\nAvailable to Brain and Workers for this account.`, [{ text: "Cancel", style: "cancel" }, { text: "Allow", onPress: commit }]);
  };
  const reconnect = () => { setAnother(true); setPage("service"); setPhase("idle"); setError(""); };
  const recover = (action: AccountRecoveryAction) => {
    if (!account) return;
    if (action === "refresh") void send({ action: "refresh", id: account.id });
    else if (action === "enable") void send({ action: "enable", id: account.id });
    else if (action === "disconnect") disconnect(account);
    else reconnect();
  };
  const sections = pluginCatalogSections(catalog, accounts, pluginJobs);
  const activeAccounts = accounts.filter((a) => a.status !== "disconnected");
  const pluginAccounts = activeAccounts.filter((a) => a.integration === selected?.id);
  const job = selected ? pluginJobs[selected.id] : undefined;
  const connecting = isConnecting(phase);
  const linked = account && !another && account.status !== "disconnected";
  const custom = selected?.id === "mcp" || selected?.id === "openapi";
  const title = page === "catalog" ? "Plugins" : page === "custom" ? "Custom services" : page === "permissions" ? "Permissions" : page === "advanced" ? "Tools & activity" : page === "accounts" ? "Connected accounts" : selected?.name ?? "Plugins";
  const catalogFailed = page === "catalog" && !catalog.length;
  const showError = Boolean(error) && !catalogFailed;
  return <View style={{ flex: 1, backgroundColor: colors.bgPrimary }}>
    <Stack.Screen options={{ title, headerLeft: () => <Button label="Back" accessibilityLabel="Back" icon="chevron-back" variant="plain" onPress={back} />, gestureEnabled: page === "catalog" }} />
    <KeyboardAwareScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled" bottomOffset={Spacing.lg}>
      {page === "catalog" && serverId ? <ServerContextRow name={serverName} connection={connection} /> : null}
      {deferredName ? <InlineNotice tone="warning" icon="swap-horizontal-outline" title={`Authorization saved for ${deferredName}`} detail="Choose that server in Settings to finish connecting. This server has not received it." action={{ label: "Settings", onPress: () => router.push("/settings") }} /> : null}
      {showError ? <InlineNotice tone={phase === "cancelled" ? "neutral" : "danger"} title={phase === "cancelled" ? "Connection cancelled" : phase === "failed" ? "Connection didn't finish" : "Request failed"} detail={error} action={page === "catalog" ? { label: "Try again", onPress: () => void send({ action: "list" }), disabled: busy } : undefined} /> : null}
      {!error && phase === "cancelled" ? <InlineNotice title="Connection cancelled" detail="You can try again." /> : null}
      {!serverId ? <NoServerState onOpenSettings={() => router.push("/settings")} /> : null}
      {page === "catalog" && serverId ? <CatalogView
        sections={sections}
        loading={busy}
        error={catalogFailed ? error : ""}
        serverName={serverName}
        onRetry={() => void send({ action: "list" })}
        onOpenAccount={(plugin, item) => void choose(plugin, item)}
        onOpenService={(plugin) => void choose(plugin)}
        onOpenCustom={() => setPage("custom")}
      /> : null}
      {page === "custom" ? <CustomCatalogView sections={sections} onOpenService={(plugin) => void choose(plugin)} /> : null}
      {page === "service" && selected ? <>
        <ServiceHeader plugin={selected} account={linked ? account : null} />
        {connecting ? <ConnectProgressCard serviceName={selected.name} phase={phase} flow={flow} onCancel={() => void cancel()} onOpen={() => void (async () => {
          if (!flow) return;
          if (flow.flow.user_code) await Clipboard.setStringAsync(flow.flow.user_code);
          await openBrowser(flow);
        })()} /> : linked ? <LinkedAccountView
          plugin={selected}
          account={account}
          job={job}
          custom={custom}
          busy={busy}
          accountCount={pluginAccounts.length}
          onRecover={recover}
          onOpenPermissions={() => setPage(custom ? "advanced" : "permissions")}
          onOpenAccounts={() => setPage("accounts")}
          onOpenTools={() => setPage("advanced")}
        /> : custom ? <CustomServiceForm key={selected.id} plugin={selected} busy={busy} serverName={serverName} send={send} authorize={authorize} /> : <ConnectOfferView
          plugin={selected}
          job={job}
          writes={writes}
          busy={busy}
          serverName={serverName}
          retry={phase === "failed" || phase === "cancelled"}
          confirmIdentity={flow?.flow.status === "confirm" ? flow.flow.identity ?? "" : null}
          onWritesChange={setWrites}
          onConnect={() => void authorize({ integration: selected.id, allow_writes: writes })}
          onUseServerAccount={selected.id === "github" ? () => void previewGitHub() : undefined}
          onImport={() => void importGitHub()}
          onChooseAnother={() => void cancel()}
          onRetryVerification={phase === "failed" && pending.current?.callback ? () => void check() : undefined}
        />}
      </> : null}
      {page === "accounts" && selected ? <AccountsView
        plugin={selected}
        accounts={pluginAccounts}
        selectedId={account?.id ?? null}
        busy={busy}
        onOpen={(item) => { void send({ action: "get", id: item.id }).then(() => { if (valid()) setPage("service"); }); }}
        onConnectAnother={reconnect}
        onDisconnect={disconnect}
      /> : null}
      {page === "permissions" && account ? <PermissionsView account={account} job={job} busy={busy} onChange={changeGroup} onReconnect={reconnect} /> : null}
      {page === "advanced" && account ? <ToolsView
        account={account}
        busy={busy}
        onToggleEnabled={(enabled) => void send({ action: enabled ? "enable" : "disable", id: account.id })}
        onRefresh={() => void send({ action: "refresh", id: account.id })}
        onToggleTool={(tool, allowed) => toggleTool(account, tool, allowed)}
      /> : null}
    </KeyboardAwareScrollView>
  </View>;
}
const styles = StyleSheet.create({ content: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 48, gap: 16 } });
