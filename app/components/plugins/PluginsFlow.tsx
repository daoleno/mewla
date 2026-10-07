import React, { createContext, useCallback, useContext, useEffect, useRef, useState, type MutableRefObject, type ReactNode } from "react";
import { Alert, AppState, Linking, StyleSheet, View } from "react-native";
import { Stack, usePathname, useRouter } from "expo-router";
import { KeyboardAwareScrollView } from "react-native-keyboard-controller";
import { openPluginAuthorization } from "../../services/pluginBrowser";
import { secureStorage } from "../../services/secureStorage";
import { useCurrentServer } from "../../store/currentServer";
import { Spacing, useAppColors } from "../../constants/tokens";
import { InlineNotice } from "../ui";
import { NoServerState } from "./PluginConnectionViews";
import { ServerContextRow, type ServerConnection } from "../extensions/ServerContextRow";
import { pluginCatalogSections, type AccountRecoveryAction } from "../../services/pluginConnectionsModel";
import { wsClient } from "../../services/websocket";
import { type ConnectionRequest, type ConnectionResponse, type PluginAccount, type PluginIntegration } from "../../services/connections";
import { finishPluginReturn, matchesPluginReturn, pendingConnectionKey, PLUGIN_CALLBACK, pluginJobs, type ConnectPhase, type PendingConnection } from "../../services/pluginOnboarding";

/** Route of one service page; its sub-pages live below it. */
export function pluginServicePath(serviceId: string) {
  return `/plugins/${encodeURIComponent(serviceId)}`;
}

type PluginsFlowValue = ReturnType<typeof usePluginsFlowState>;
const PluginsFlowContext = createContext<PluginsFlowValue | null>(null);

export function usePluginsFlow(): PluginsFlowValue {
  const value = useContext(PluginsFlowContext);
  if (!value) throw new Error("usePluginsFlow must be used inside the Plugins layout");
  return value;
}

interface PluginsFlowProps {
  serverId: string | null;
  serverName: string;
  connection: ServerConnection;
  deferredName?: string;
  deferReturn: (url: string) => Promise<void>;
  /** The current server, read when a service page leaves (after a switch, it is the new one). */
  currentServerRef: MutableRefObject<string | null>;
}

/**
 * One connection flow for every Plugins route. The routes are only views:
 * the catalog, pending authorization and the selected account live here so a
 * push or pop between pages never loses them. Keyed by server in the layout.
 */
export function PluginsFlowProvider({ children, ...props }: PluginsFlowProps & { children: ReactNode }) {
  const value = usePluginsFlowState(props);
  return <PluginsFlowContext.Provider value={value}>{children}</PluginsFlowContext.Provider>;
}

function usePluginsFlowState({ serverId, serverName, connection, deferredName, deferReturn, currentServerRef }: PluginsFlowProps) {
  const PENDING_KEY = pendingConnectionKey(serverId);
  const router = useRouter();
  const pathname = usePathname();
  const pathnameRef = useRef(pathname);
  useEffect(() => { pathnameRef.current = pathname; }, [pathname]);
  const { isCurrentServer } = useCurrentServer();
  const [catalog, setCatalog] = useState<PluginIntegration[]>([]);
  const [accounts, setAccounts] = useState<PluginAccount[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [account, setAccount] = useState<PluginAccount | null>(null);
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
  const selectedRef = useRef<string | null>(null);
  const openedAccountRef = useRef<string | undefined>(undefined);
  const accountsRef = useRef<PluginAccount[]>([]);
  const valid = useCallback(() => alive.current && !!serverId && isCurrentServer(serverId), [serverId, isCurrentServer]);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const remember = useCallback(async (value: PendingConnection | null) => {
    pending.current = value; setFlow(value);
    if (value) await secureStorage.setItemAsync(PENDING_KEY, JSON.stringify(value));
    else await secureStorage.deleteItemAsync(PENDING_KEY);
  }, [PENDING_KEY]);
  const apply = useCallback((response: ConnectionResponse) => {
    if (!valid()) return;
    if (response.catalog) { setCatalog(response.catalog); setLoaded(true); }
    if (response.accounts) { accountsRef.current = response.accounts; setAccounts(response.accounts); }
    if (response.account) {
      const next = response.account;
      setAccount(next);
      setAccounts((old) => {
        const merged = [...old.filter((item) => item.id !== next.id), next];
        accountsRef.current = merged;
        return merged;
      });
      setAnother(false);
    }
  }, [valid]);
  const applyRef = useRef(apply);
  useEffect(() => { applyRef.current = apply; }, [apply]);
  const send = useCallback(async (request: ConnectionRequest): Promise<ConnectionResponse | undefined> => {
    if (!serverId || !valid() || mutation.current) return;
    mutation.current = true; setBusy(true); setError("");
    try { const result = await wsClient.requestConnections(serverId, request); if (valid()) { apply(result); return result; } }
    catch (failure) {
      if (valid()) setError(failure instanceof Error ? failure.message : "Could not reach this server. Try again.");
      // A failed first list still ends loading; the catalog shows the error.
      if (request.action === "list" && valid()) setLoaded(true);
    }
    finally { mutation.current = false; if (valid()) setBusy(false); }
  }, [apply, serverId, valid]);
  const settle = useCallback(async (result: ConnectionResponse) => {
    if (!valid()) return;
    apply(result);
    if (result.flow?.status === "connected" && result.account) {
      setPhase("connected"); await remember(null);
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
    void secureStorage.getItemAsync(PENDING_KEY).then(async (raw) => {
      if (!raw || !valid()) return;
      try {
        const stored = JSON.parse(raw) as PendingConnection;
        if (stored.serverId !== serverId || Date.parse(stored.flow.expires) <= Date.now()) return;
        pending.current = stored; setFlow(stored); setPhase(stored.flow.status === "confirm" ? "idle" : "waiting");
        const list = await wsClient.requestConnections(serverId!, { action: "list" });
        if (!valid()) return;
        apply(list);
        const service = stored.flow.integration;
        selectedRef.current = service; openedAccountRef.current = undefined; setSelectedId(service);
        if (!pathnameRef.current.startsWith(pluginServicePath(service))) router.push(pluginServicePath(service));
        const initial = await Linking.getInitialURL();
        if (initial && matchesPluginReturn(initial, stored.flow)) await finish(initial); else await check();
      } catch { if (valid()) { setError("The previous connection could not be restored. Connect again."); await remember(null); } }
    });
    const connected = (event: { serverId: string }) => { if (event.serverId === serverId) { void send({ action: "list" }); void check(); } };
    const subscription = AppState.addEventListener("change", (state) => { if (state === "active") void check(); });
    const links = Linking.addEventListener("url", (event) => void finish(event.url));
    wsClient.on("connected", connected);
    return () => { subscription.remove(); links.remove(); wsClient.off("connected", connected); };
  }, [apply, check, finish, remember, router, send, serverId, valid]);
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
  /** A service route opened (or its sub-page): select it and load its account once. */
  const openService = useCallback(async (serviceId: string, accountId?: string) => {
    // Sub-pages pass no account and keep whatever the service page opened.
    if (selectedRef.current === serviceId && (!accountId || openedAccountRef.current === accountId)) return;
    selectedRef.current = serviceId; openedAccountRef.current = accountId;
    setSelectedId(serviceId); setAccount(null); setAnother(false); setError(""); setWrites(false);
    if (pending.current?.flow.integration !== serviceId) setPhase("idle");
    const existing = accountId ?? accountsRef.current.find((item) => item.integration === serviceId && item.status !== "disconnected")?.id;
    if (existing) await send({ action: "get", id: existing });
  }, [send]);
  /**
   * The service page left the stack (Back, browser Back or swipe): drop its
   * selection and cancel its unfinished connection. A server switch remounts
   * this provider instead, and keeps the old server's flow for its return.
   */
  const leaveService = useCallback((serviceId: string) => {
    if (selectedRef.current === serviceId) { selectedRef.current = null; openedAccountRef.current = undefined; }
    const active = pending.current;
    if (!active || active.flow.integration !== serviceId || active.serverId !== currentServerRef.current) return;
    attempt.current++;
    pending.current = null;
    void secureStorage.deleteItemAsync(pendingConnectionKey(active.serverId));
    void wsClient.requestConnections(active.serverId, { action: "connect_cancel", flow_id: active.flow.id })
      .then((response) => { if (response.account) applyRef.current(response); })
      .catch(() => undefined);
    if (alive.current) { setFlow(null); setPhase("idle"); setError(""); }
  }, [currentServerRef]);
  const changeGroup = (group: "read" | "write", allowed: boolean) => {
    if (!account) return;
    const id = account.id;
    const commit = () => { if (valid()) void send({ action: "permissions", id, group, allowed }); };
    if (allowed && group === "write") Alert.alert("Allow changes when asked?", pluginJobs[account.integration]?.write + ". Brain still needs your instruction before taking action.", [{ text: "Cancel", style: "cancel" }, { text: "Allow changes", onPress: commit }]);
    else commit();
  };
  const disconnect = (target: PluginAccount, after?: () => void) => Alert.alert("Disconnect this account?", "Future calls stop immediately. Zen removes its saved credential and revokes access where supported.", [{ text: "Cancel", style: "cancel" }, { text: "Disconnect", style: "destructive", onPress: () => { if (valid()) void send({ action: "disconnect", id: target.id }).then((response) => { if (response && valid()) { setAccount(null); after?.(); } }); } }]);
  const toggleTool = (target: PluginAccount, tool: NonNullable<PluginAccount["tools"]>[number], allowed: boolean) => {
    const commit = () => { if (valid()) void send({ action: "policy", id: target.id, tool: tool.name, allowed }); };
    if (!allowed) commit(); else Alert.alert("Allow this tool?", `${tool.description}\n\nAvailable to Brain and Workers for this account.`, [{ text: "Cancel", style: "cancel" }, { text: "Allow", onPress: commit }]);
  };
  const reconnect = () => { setAnother(true); setPhase("idle"); setError(""); };
  const recover = (action: AccountRecoveryAction) => {
    if (!account) return;
    if (action === "refresh") void send({ action: "refresh", id: account.id });
    else if (action === "enable") void send({ action: "enable", id: account.id });
    else if (action === "disconnect") disconnect(account);
    else reconnect();
  };
  return {
    serverId, serverName, connection, deferredName,
    catalog, accounts, loaded, account, busy, error, phase, flow, writes, another,
    selected: catalog.find((plugin) => plugin.id === selectedId) ?? null,
    sections: pluginCatalogSections(catalog, accounts, pluginJobs),
    send, cancel, check, openBrowser, authorize, previewGitHub, importGitHub, setWrites,
    openService, leaveService, changeGroup, disconnect, toggleTool, reconnect, recover,
  };
}

/**
 * Binds a Plugins service route to the flow. The service page itself owns
 * leaving; its sub-pages only make sure the service is selected.
 */
export function usePluginService(serviceId: string, options: { accountId?: string; owner?: boolean } = {}) {
  const { loaded, openService, leaveService } = usePluginsFlow();
  const { accountId, owner = false } = options;
  useEffect(() => {
    if (loaded && serviceId) void openService(serviceId, accountId);
  }, [accountId, loaded, openService, serviceId]);
  useEffect(() => {
    if (!owner) return;
    return () => leaveService(serviceId);
  }, [leaveService, owner, serviceId]);
}

/** Shared frame for every Plugins route: title, notices and one keyboard-aware scroll view. */
export function PluginsPage({ title, catalog = false, children }: { title: string; catalog?: boolean; children?: ReactNode }) {
  const colors = useAppColors();
  const router = useRouter();
  const flow = usePluginsFlow();
  const catalogFailed = catalog && !flow.catalog.length;
  const showError = Boolean(flow.error) && !catalogFailed;
  return <View style={{ flex: 1, backgroundColor: colors.bgPrimary }}>
    <Stack.Screen options={{ title }} />
    <KeyboardAwareScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled" bottomOffset={Spacing.lg}>
      {catalog && flow.serverId ? <ServerContextRow name={flow.serverName} connection={flow.connection} /> : null}
      {flow.deferredName ? <InlineNotice tone="warning" icon="swap-horizontal-outline" title={`Authorization saved for ${flow.deferredName}`} detail="Switch to that server in Settings to finish." action={{ label: "Settings", onPress: () => router.push("/settings") }} /> : null}
      {showError ? <InlineNotice tone={flow.phase === "cancelled" ? "neutral" : "danger"} title={flow.phase === "cancelled" ? "Connection cancelled" : flow.phase === "failed" ? "Connection didn't finish" : "Request failed"} detail={flow.error} action={catalog ? { label: "Try again", onPress: () => void flow.send({ action: "list" }), disabled: flow.busy } : undefined} /> : null}
      {!flow.error && flow.phase === "cancelled" ? <InlineNotice title="Connection cancelled" detail="You can try again." /> : null}
      {!flow.serverId ? <NoServerState onOpenSettings={() => router.push("/settings")} /> : children}
    </KeyboardAwareScrollView>
  </View>;
}
const styles = StyleSheet.create({ content: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 48, gap: 16, width: "100%", maxWidth: 760, alignSelf: "center" } });
