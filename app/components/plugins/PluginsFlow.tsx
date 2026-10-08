import React, { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { Alert, AppState, Linking, Platform, StyleSheet, View } from "react-native";
import { Stack, useGlobalSearchParams, useRouter } from "expo-router";
import { KeyboardAwareScrollView } from "react-native-keyboard-controller";
import * as Clipboard from "expo-clipboard";
import { openPluginAuthorization } from "../../services/pluginBrowser";
import { secureStorage } from "../../services/secureStorage";
import { useCurrentServer } from "../../store/currentServer";
import { Spacing, useAppColors } from "../../constants/tokens";
import { InlineNotice } from "../ui";
import { ConnectStatusCard, NoServerState } from "./PluginConnectionViews";
import { ServerOfflineNotice, type ServerConnection } from "../extensions/ServerOfflineNotice";
import { pluginCatalogRows, type AccountRecoveryAction } from "../../services/pluginConnectionsModel";
import { wsClient } from "../../services/websocket";
import { type ConnectionRequest, type ConnectionResponse, type PluginAccount, type PluginIntegration } from "../../services/connections";
import { connectInput, finishPluginReturn, isPluginCallbackUrl, matchesPluginReturn, pendingConnectionKey, pluginJobs, pluginReturnError, type ConnectPhase, type PendingConnection } from "../../services/pluginOnboarding";

/** Route of one service page; its Tools page lives below it. */
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
}

/**
 * One connection flow for every Plugins route. The routes are only views:
 * the catalog, the one sign-in in progress and its outcome live here, so a
 * push or pop between pages never loses them. Keyed by server in the layout.
 */
export function PluginsFlowProvider({ children, ...props }: PluginsFlowProps & { children: ReactNode }) {
  const value = usePluginsFlowState(props);
  return <PluginsFlowContext.Provider value={value}>{children}</PluginsFlowContext.Provider>;
}

/** Options of the one connect action: ask for changes too, or a custom service's details. */
export type ConnectOptions = {
  writes?: boolean;
  input?: NonNullable<ConnectionRequest["input"]>;
  /** False for a custom service reached with a token or none: it connects without a sign-in. */
  signIn?: boolean;
};

function usePluginsFlowState({ serverId, serverName, connection, deferredName, deferReturn }: PluginsFlowProps) {
  const PENDING_KEY = pendingConnectionKey(serverId);
  const router = useRouter();
  // A web sign-in comes back as /plugins?connected=… or ?plugin_error=…, with service=.
  const returnParams = useRef(useGlobalSearchParams<{ plugin_error?: string; connected?: string; service?: string }>()).current;
  const { isCurrentServer } = useCurrentServer();
  const [catalog, setCatalog] = useState<PluginIntegration[]>([]);
  const [accounts, setAccounts] = useState<PluginAccount[]>([]);
  const [loaded, setLoaded] = useState(false);
  /** Key of the request in flight, so only the control that started it spins. */
  const [running, setRunning] = useState<string | null>(null);
  /** A request that failed outside a sign-in (a permission, disconnect or list). */
  const [error, setError] = useState("");
  const [phase, setPhase] = useState<ConnectPhase>("idle");
  /** The service the sign-in is (or was last) for, and what went wrong. */
  const [service, setService] = useState<string | null>(null);
  const [connectError, setConnectError] = useState("");
  const [connectedId, setConnectedId] = useState<string | null>(null);
  const [flow, setFlow] = useState<PendingConnection | null>(null);
  const [details, setDetails] = useState<Record<string, PluginAccount>>({});
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
    if (value) await secureStorage.setItemAsync(PENDING_KEY, JSON.stringify(value));
    else await secureStorage.deleteItemAsync(PENDING_KEY);
  }, [PENDING_KEY]);
  const apply = useCallback((response: ConnectionResponse) => {
    if (!valid()) return;
    if (response.catalog) { setCatalog(response.catalog); setLoaded(true); }
    if (response.accounts) setAccounts(response.accounts);
    if (response.account) {
      const next = response.account;
      setAccounts((old) => [...old.filter((item) => item.id !== next.id), { ...next, tools: undefined }]);
      if (next.tools) setDetails((old) => ({ ...old, [next.id]: next }));
    }
  }, [valid]);
  const send = useCallback(async (request: ConnectionRequest, key?: string): Promise<ConnectionResponse | undefined> => {
    if (!serverId || !valid() || mutation.current) return;
    mutation.current = true; setRunning(key ?? request.action); setError("");
    try { const result = await wsClient.requestConnections(serverId, request); if (valid()) { apply(result); return result; } }
    catch (failure) {
      if (valid()) setError(failure instanceof Error ? failure.message : "Could not reach this server. Try again.");
      // A failed first list still ends loading; the list shows the error.
      if (request.action === "list" && valid()) setLoaded(true);
    }
    finally { mutation.current = false; if (valid()) setRunning(null); }
  }, [apply, serverId, valid]);
  const fail = useCallback((message: string, status: "failed" | "cancelled" = "failed") => {
    if (!valid()) return;
    setPhase(status); setConnectError(message);
  }, [valid]);
  const settle = useCallback(async (result: ConnectionResponse) => {
    if (!valid()) return;
    apply(result);
    if (result.flow?.status === "connected" && result.account) {
      setService(result.flow.integration); setConnectedId(result.account.id); setPhase("connected"); setConnectError("");
      await remember(null);
    } else if (result.flow?.status === "failed" || result.flow?.status === "cancelled") {
      fail(result.flow.message ?? "Connection cancelled. No new account was connected.", result.flow.status);
      await remember(null);
    } else if (result.flow?.status === "waiting") {
      setPhase("waiting"); setConnectError("");
    }
  }, [apply, fail, remember, valid]);
  const finish = useCallback(async (url: string) => {
    const active = pending.current;
    if (!active || !valid() || !matchesPluginReturn(url, active.flow)) return;
    if (completion.current) { await remember({ ...active, callback: url }); return; }
    completion.current = true; setPhase("verifying"); setConnectError("");
    try {
      await remember({ ...active, callback: url });
      const response = await finishPluginReturn(active, serverId, url, (id, request) => wsClient.requestConnections(id, request));
      await settle(response);
    } catch (failure) {
      fail(failure instanceof Error ? failure.message : "Could not verify authorization. Try again.");
    } finally { completion.current = false; }
  }, [fail, remember, serverId, settle, valid]);
  const check = useCallback(async () => {
    const active = pending.current;
    if (!active || !valid() || completion.current) return;
    if (Date.parse(active.flow.expires) <= Date.now()) {
      fail("This sign-in expired. Start again."); await remember(null); return;
    }
    if (active.callback) { await finish(active.callback); return; }
    completion.current = true;
    try { await settle(await wsClient.requestConnections(active.serverId, { action: "connect_status", flow_id: active.flow.id })); }
    catch (failure) {
      // The server answered: it no longer has this sign-in (it expired or restarted).
      if ((failure as { code?: string }).code === "plugin_request_failed") {
        fail(failure instanceof Error ? failure.message : "This sign-in expired. Start again."); await remember(null);
      // The server can't be asked yet (a page load, a reconnect). The sign-in
      // still waits there; reconnecting checks it again.
      } else if (valid() && pending.current) setPhase("waiting");
    }
    finally {
      completion.current = false;
      if (pending.current?.callback && valid()) void finish(pending.current.callback);
    }
  }, [fail, finish, remember, settle, valid]);
  useEffect(() => {
    void send({ action: "list" });
    // A web sign-in returned through the daemon's callback. The outcome is
    // already settled there; the stored flow, if any, only confirms it.
    const { connected, plugin_error: returnedError, service: returnedService } = returnParams;
    if (connected || returnedError) {
      // Drop the outcome from the address, so a reload or Back doesn't replay it.
      if (Platform.OS === "web") globalThis.history?.replaceState(globalThis.history.state, "", globalThis.location.pathname);
      else router.setParams({ plugin_error: undefined, connected: undefined, service: undefined });
      setService(returnedService ?? null);
      if (connected) { setConnectedId(connected); setPhase("connected"); }
      else fail(pluginReturnError(returnedError) ?? "", returnedError === "cancelled" ? "cancelled" : "failed");
    }
    void secureStorage.getItemAsync(PENDING_KEY).then(async (raw) => {
      if (!raw || !valid()) return;
      try {
        const stored = JSON.parse(raw) as PendingConnection;
        if (stored.serverId !== serverId || Date.parse(stored.flow.expires) <= Date.now()) { await remember(null); return; }
        if (connected || returnedError) {
          // The return settled this flow; a different one keeps waiting.
          if (!returnedService || stored.flow.integration === returnedService) { await remember(null); return; }
        }
        pending.current = stored; setFlow(stored); setService(stored.flow.integration);
        setPhase(stored.flow.status === "confirm" ? "idle" : "waiting");
        const initial = await Linking.getInitialURL();
        if (initial && matchesPluginReturn(initial, stored.flow)) await finish(initial); else await check();
      } catch { if (valid()) { fail("The previous sign-in could not be restored. Start again."); await remember(null); } }
    });
    const connectedEvent = (event: { serverId: string }) => { if (event.serverId === serverId) { void send({ action: "list" }); void check(); } };
    const subscription = AppState.addEventListener("change", (state) => { if (state === "active") void check(); });
    const links = Linking.addEventListener("url", (event) => void finish(event.url));
    // A web tab coming back into view checks the sign-in it left waiting.
    const visible = () => { if (globalThis.document?.visibilityState === "visible") void check(); };
    globalThis.document?.addEventListener?.("visibilitychange", visible);
    wsClient.on("connected", connectedEvent);
    return () => {
      subscription.remove(); links.remove(); wsClient.off("connected", connectedEvent);
      globalThis.document?.removeEventListener?.("visibilitychange", visible);
    };
  }, [check, fail, finish, remember, router, send, serverId, valid]);
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
    if (valid()) { await remember(null); fail("Sign-in cancelled. Nothing was connected.", "cancelled"); }
  };
  const openBrowser = async (active: PendingConnection) => {
    if (!active.flow.authorization_url || !valid() || browserOpen.current) return;
    browserOpen.current = true;
    try {
      setPhase("waiting");
      if (active.flow.user_code) await Clipboard.setStringAsync(active.flow.user_code);
      const result = await openPluginAuthorization(active.flow.authorization_url, !!active.flow.user_code, Platform.OS === "web" ? active.flow.web_return ? "same-tab" : "new-tab" : undefined);
      if (result.type === "external") { await check(); return; }
      if (!valid()) {
        if (result.type === "success") await deferReturn(result.url);
        return;
      }
      if (pending.current?.flow.id !== active.flow.id) return;
      if (result.type === "success") {
        if (matchesPluginReturn(result.url, active.flow)) await finish(result.url);
        else if (isPluginCallbackUrl(result.url)) await check();
      }
      else if (!completion.current) await cancel();
    } catch { fail("The browser could not open. Try again."); }
    finally { browserOpen.current = false; }
  };
  /**
   * The one connect action, for a new service, another account, Reconnect,
   * or allowing changes that the service has to approve. Built-in services
   * open their own sign-in; a custom service brings its form's details.
   */
  const connect = async (serviceId: string, options: ConnectOptions = {}) => {
    if (mutation.current || starting.current || !valid()) return;
    starting.current = true;
    try {
      if (pending.current) await cancel();
      const currentAttempt = ++attempt.current;
      setService(serviceId); setConnectedId(null); setPhase("opening"); setConnectError("");
      const input = options.input ?? { integration: serviceId, allow_writes: !!options.writes };
      // A custom service with a token connects directly, without a sign-in.
      if (options.signIn === false) {
        const added = await send({ action: "add", input }, `connect:${serviceId}`);
        if (added?.account && valid()) { setConnectedId(added.account.id); setPhase("connected"); }
        else if (valid()) setPhase("idle");
        return;
      }
      const result = await send({ action: "connect_start", input: connectInput(input, Platform.OS, globalThis.location?.origin) }, `connect:${serviceId}`);
      if (!result?.flow || !valid()) { if (valid()) { setPhase("idle"); } return; }
      if (currentAttempt !== attempt.current) {
        await wsClient.requestConnections(serverId!, { action: "connect_cancel", flow_id: result.flow.id }).catch(() => undefined);
        return;
      }
      const active = { serverId: serverId!, flow: result.flow };
      await remember(active);
      // Device authorization asks for a code on GitHub. Let people see and
      // copy it before leaving Mewla, instead of opening a page with no code.
      if (active.flow.user_code) setPhase("waiting");
      else await openBrowser(active);
    } catch (failure) {
      fail(failure instanceof Error ? failure.message : "Could not start this connection. Try again.");
    } finally { starting.current = false; }
  };
  /** GitHub only: use the account `gh` is signed in to on the server instead of a code. */
  const useServerGitHub = async () => {
    if (pending.current) await cancel();
    setService("github"); setConnectError("");
    const result = await send({ action: "github_preview" }, "connect:github");
    if (result?.flow && valid()) { await remember({ serverId: serverId!, flow: result.flow }); setPhase("idle"); }
    else if (valid()) fail("No GitHub login was found on this server.");
  };
  const importGitHub = async () => {
    if (!pending.current || mutation.current) return;
    setPhase("verifying");
    const result = await send({ action: "github_import", flow_id: pending.current.flow.id, input: { integration: "github" } }, "connect:github");
    if (result) await settle(result); else fail("GitHub couldn't be verified. Start again.");
  };
  /** Clears the outcome of the last sign-in once it has been seen. */
  const dismiss = () => { if (!pending.current) { setPhase("idle"); setConnectError(""); setConnectedId(null); } };
  /**
   * Read and search or Make changes. Allowing changes that the service never
   * granted is the connect action again, which the service approves.
   */
  const setAccess = (account: PluginAccount, group: "read" | "write", allowed: boolean) => {
    if (group === "write" && allowed && account.access?.write_consent) { void connect(account.integration, { writes: true }); return; }
    void send({ action: "permissions", id: account.id, group, allowed }, `${group}:${account.id}`);
  };
  const disconnect = async (account: PluginAccount) => {
    const response = await send({ action: "disconnect", id: account.id }, `disconnect:${account.id}`);
    if (response && valid() && connectedId === account.id) dismiss();
    return !!response;
  };
  const recover = (account: PluginAccount, action: AccountRecoveryAction) => {
    if (action === "refresh") void send({ action: "refresh", id: account.id }, `recover:${account.id}`);
    else if (action === "enable") void send({ action: "enable", id: account.id }, `recover:${account.id}`);
    else if (action === "disconnect") void disconnect(account);
    else void connect(account.integration);
  };
  /** Loads one account's tools and history, for its Tools page. */
  const load = useCallback((id: string) => send({ action: "get", id }, `load:${id}`), [send]);
  /** One tool by name. A tool outside the reviewed groups shows what it does before it is allowed. */
  const toggleTool = (target: PluginAccount, tool: NonNullable<PluginAccount["tools"]>[number], allowed: boolean) => {
    const commit = () => { if (valid()) void send({ action: "policy", id: target.id, tool: tool.name, allowed }, `tool:${target.id}:${tool.name}`); };
    if (!allowed) commit(); else Alert.alert("Allow this tool?", `${tool.description}\n\nAvailable to Brain and Workers for this account.`, [{ text: "Cancel", style: "cancel" }, { text: "Allow", onPress: commit }]);
  };
  const plugin = (id: string | null) => catalog.find((item) => item.id === id) ?? null;
  return {
    serverId, serverName, connection, deferredName,
    catalog, accounts, details, loaded, running, busy: running !== null, error, phase, flow, service, connectError,
    connected: accounts.find((account) => account.id === connectedId) ?? null,
    rows: pluginCatalogRows(catalog, accounts, pluginJobs),
    plugin, send, connect, cancel, check, openBrowser, useServerGitHub, importGitHub, dismiss,
    setAccess, disconnect, recover, load, toggleTool,
  };
}

/** Shared frame for every Plugins route: title, notices, the sign-in in progress and one keyboard-aware scroll view. */
export function PluginsPage({ title, catalog = false, children }: { title: string; catalog?: boolean; children?: ReactNode }) {
  const colors = useAppColors();
  const router = useRouter();
  const flow = usePluginsFlow();
  const catalogFailed = catalog && !flow.catalog.length;
  return <View style={{ flex: 1, backgroundColor: colors.bgPrimary }}>
    <Stack.Screen options={{ title }} />
    <KeyboardAwareScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled" bottomOffset={Spacing.lg}>
      {catalog && flow.serverId ? <ServerOfflineNotice name={flow.serverName} connection={flow.connection} /> : null}
      {flow.deferredName ? <InlineNotice tone="warning" icon="swap-horizontal" title={`Authorization saved for ${flow.deferredName}`} detail="Switch to that server in Settings to finish." action={{ label: "Settings", onPress: () => router.push("/settings") }} /> : null}
      {flow.error && !catalogFailed ? <InlineNotice tone="danger" title="That didn't go through" detail={flow.error} /> : null}
      {flow.serverId && flow.service ? <ConnectStatusCard
        serviceName={flow.plugin(flow.service)?.name ?? "the service"}
        phase={flow.phase}
        flow={flow.flow}
        error={flow.connectError}
        connected={flow.connected}
        job={pluginJobs[flow.service]}
        starting={flow.running === `connect:${flow.service}`}
        allowing={!!flow.connected && flow.running === `write:${flow.connected.id}`}
        onOpen={() => { if (flow.flow) void flow.openBrowser(flow.flow); }}
        onCancel={() => void flow.cancel()}
        onRetry={() => {
          if (!flow.service) return;
          // A custom service starts again from its form.
          if (flow.service === "mcp" || flow.service === "openapi") { flow.dismiss(); router.push(pluginServicePath(flow.service)); }
          else void flow.connect(flow.service);
        }}
        onDismiss={flow.dismiss}
        onAllowChanges={(account) => flow.setAccess(account, "write", true)}
        onChooseTools={(account) => { flow.dismiss(); router.push({ pathname: `${pluginServicePath(account.integration)}/tools`, params: { account: account.id } } as never); }}
        onUseServerAccount={flow.service === "github" ? () => void flow.useServerGitHub() : undefined}
        onImport={() => void flow.importGitHub()}
      /> : null}
      {!flow.serverId ? <NoServerState onOpenSettings={() => router.push("/settings")} /> : children}
    </KeyboardAwareScrollView>
  </View>;
}
const styles = StyleSheet.create({ content: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 48, gap: 16, width: "100%", maxWidth: 760, alignSelf: "center" } });
