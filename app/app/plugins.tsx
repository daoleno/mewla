import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, AppState, Linking, ScrollView, StyleSheet, Switch, TextInput, View } from "react-native";
import { Stack } from "expo-router";
import { useCurrentServer } from "../store/currentServer";
import { useAppColors } from "../constants/tokens";
import { AppText, Button, ListRow, ListSection } from "../components/ui";
import { wsClient } from "../services/websocket";
import { accountStatus, type ConnectionRequest, type ConnectionResponse, type PluginAccount, type PluginIntegration } from "../services/connections";

export default function PluginsScreen() {
  const { currentServerId } = useCurrentServer();
  // Remount all account details, pending requests and secret fields on switch.
  return <PluginCatalog key={currentServerId ?? "none"} serverId={currentServerId} />;
}

function PluginCatalog({ serverId }: { serverId: string | null }) {
  const colors = useAppColors();
  const { isCurrentServer } = useCurrentServer();
  const [catalog, setCatalog] = useState<PluginIntegration[]>([]);
  const [accounts, setAccounts] = useState<PluginAccount[]>([]);
  const [selected, setSelected] = useState<PluginIntegration | null>(null);
  const [account, setAccount] = useState<PluginAccount | null>(null);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [credential, setCredential] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [spec, setSpec] = useState("");
  const [oauthConfigured, setOauthConfigured] = useState<string[]>([]);
  const [tokenMode, setTokenMode] = useState(false);
  const [allowWrites, setAllowWrites] = useState(false);
  const [trustInternal, setTrustInternal] = useState(false);
  const [networks, setNetworks] = useState("");
  const [configuring, setConfiguring] = useState(false);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [callbackUrl, setCallbackUrl] = useState("");
  const oauthSupported = !!selected && ["google", "notion", "linear", "mcp"].includes(selected.id);
  const browserAuth = oauthSupported && !tokenMode;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const accountIdRef = useRef<string | undefined>(undefined);
  useEffect(() => { accountIdRef.current = account?.id; }, [account?.id]);
  const alive = useRef(true);
  const pending = useRef(false);
  const valid = useCallback(() => alive.current && !!serverId && isCurrentServer(serverId), [serverId, isCurrentServer]);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);

  const request = useCallback(async (input: ConnectionRequest): Promise<ConnectionResponse | undefined> => {
    if (!serverId || !valid() || pending.current) return;
    pending.current = true; setBusy(true); setError("");
    try {
      const response = await wsClient.requestConnections(serverId, input);
      if (!valid()) return;
      if (response.oauth_configured) setOauthConfigured(response.oauth_configured);
      if (response.catalog) setCatalog(response.catalog);
      if (input.action === "list") setAccounts(response.accounts ?? []);
      if (response.account) {
        setAccount(response.account);
        setAccounts((previous) => [...previous.filter((item) => item.id !== response.account!.id), response.account!]);
      }
      return response;
    } catch (failure) {
      if (valid()) setError(failure instanceof Error ? failure.message : "Plugin request failed.");
    } finally {
      pending.current = false;
      if (valid()) setBusy(false);
    }
  }, [serverId, valid]);

  const reload = useCallback(async () => {
    const response = await request({ action: "list" });
    const id = accountIdRef.current;
    if (response && id && valid()) await request({ action: "get", id });
  }, [request, valid]);
  useEffect(() => {
    void reload();
    const onConnected = (event: { serverId: string }) => { if (event.serverId === serverId) void reload(); };
    wsClient.on("connected", onConnected);
    return () => wsClient.off("connected", onConnected);
  }, [reload, serverId]);

  useEffect(() => {
    const subscription = AppState.addEventListener("change", (state) => {
      if (state === "active" && valid()) void reload();
    });
    return () => subscription.remove();
  }, [reload, valid]);
  const clearForm = () => {
    setAdding(false); setCredential(""); setName(""); setEndpoint(""); setSpec("");
    setTokenMode(false); setAllowWrites(false); setTrustInternal(false); setNetworks("");
    setConfiguring(false); setClientId(""); setClientSecret(""); setCallbackUrl("");
  };
  const saveOAuthConfig = async () => {
    if (!selected) return;
    const secret = clientSecret; setClientSecret("");
    const response = await request({ action: "oauth_configure", input: { integration: selected.id, name: "", oauth_client: { client_id: clientId.trim(), client_secret: secret, redirect_url: callbackUrl.trim(), resource_url: selected.id === "mcp" ? endpoint.trim() : undefined }, trusted_networks: trustInternal ? networks.split(",").map((value) => value.trim()).filter(Boolean) : undefined } });
    if (response && valid()) setConfiguring(false);
  };
  const connect = async () => {
    if (!selected) return;
    let document: unknown;
    if (selected.id === "openapi") {
      try { document = JSON.parse(spec); } catch { setError("Enter a valid OpenAPI JSON document."); return; }
    }
    const secret = credential; setCredential("");
    const response = await request({ action: browserAuth ? "oauth_start" : "add", input: { integration: selected.id, name: name.trim(), credential: secret, endpoint: endpoint.trim(), spec: document, allow_writes: allowWrites, trusted_networks: trustInternal ? networks.split(",").map((value) => value.trim()).filter(Boolean) : undefined } });
    if (response && valid()) {
      clearForm();
      if (response.authorization_url) {
        try { await Linking.openURL(response.authorization_url); }
        catch { if (valid()) setError("Could not open the system browser. Connect again to retry."); }
      }
    }
  };
  const changePolicy = (tool: string, allowed: boolean) => {
    if (!account) return;
    const id = account.id;
    if (!allowed) { void request({ action: "policy", id, tool, allowed }); return; }
    Alert.alert("Enable tool?", `${tool} will be available to Brain and Workers using ${account.name}. This can allow changes in the external service.`, [
      { text: "Cancel", style: "cancel" },
      { text: "Enable", onPress: () => { if (valid()) void request({ action: "policy", id, tool, allowed }); } },
    ]);
  };
  const inputStyle = [styles.input, { color: colors.textPrimary, backgroundColor: colors.bgSurface, borderColor: colors.border }];
  const pluginAccounts = accounts.filter((item) => item.integration === selected?.id);

  return (
    <ScrollView style={{ flex: 1, backgroundColor: colors.bgPrimary }} contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
      <Stack.Screen options={{ title: "Plugins" }} />
      <AppText variant="title">{selected?.name ?? "Plugins"}</AppText>
      <AppText tone="secondary">{selected ? selected.description : "External services and tools for Brain and Workers."}</AppText>
      {!serverId ? <AppText tone="secondary">Choose a current server in Settings to manage plugins.</AppText> : null}
      {error ? <View accessibilityRole="alert"><AppText style={{ color: colors.dangerText }}>{error}</AppText></View> : null}
      {selected ? <Button label="All plugins" variant="plain" size="sm" onPress={() => { setSelected(null); setAccount(null); clearForm(); setError(""); }} disabled={busy} /> : null}
      {!selected && serverId ? <>
        <ListSection title="Your plugins">
          {catalog.filter((plugin) => accounts.some((item) => item.integration === plugin.id && item.status !== "disconnected")).map((plugin) => (
            <ListRow key={plugin.id} title={plugin.name} subtitle={`${accounts.filter((item) => item.integration === plugin.id && item.status !== "disconnected").length} account(s)`} icon="extension-puzzle-outline" accessory="chevron" onPress={() => { setSelected(plugin); setAccount(null); }} />
          ))}
          {!accounts.some((item) => item.status !== "disconnected") ? <ListRow title={busy ? "Loading plugins…" : "No accounts connected"} subtitle="Add a plugin to give Brain access to an external service." /> : null}
        </ListSection>
        <ListSection title="Add plugin" footer="Connect an account once. Use its tools from Brain and delegated Workers.">
          {catalog.map((plugin) => <ListRow key={plugin.id} title={plugin.name} subtitle={plugin.description} value={plugin.id === "google" && !oauthConfigured.includes("google") ? "Set up authorization" : plugin.available ? undefined : "Setup required"} accessory="chevron" onPress={() => { setSelected(plugin); setAccount(null); }} />)}
        </ListSection>
        <Button label="Refresh" variant="plain" loading={busy} onPress={() => void request({ action: "list" })} />
      </> : null}
      {selected ? <>
        {pluginAccounts.length > 0 ? <ListSection title="Accounts">
          {pluginAccounts.map((item) => <ListRow key={item.id} title={item.name} subtitle={item.identity} value={accountStatus(item)} selected={account?.id === item.id} onPress={() => { clearForm(); void request({ action: "get", id: item.id }); }} disabled={busy} accessory="chevron" />)}
        </ListSection> : null}
        {!selected.available ? <ListSection title="Authorization setup required" footer="This service is recommended, but its built-in account authorization is not available yet.">
          <ListRow title="View official setup" icon="open-outline" onPress={() => void Linking.openURL(selected.setup_url)} />
        </ListSection> : !adding ? <Button label="Connect account" icon="add" disabled={busy} onPress={() => { setAdding(true); setAccount(null); setName(selected.name); }} /> : <View style={styles.form}>
          <TextInput accessibilityLabel="Account name" placeholder="Account name" placeholderTextColor={colors.textTertiary} value={name} onChangeText={setName} maxLength={80} style={inputStyle} />
          {selected.id === "mcp" || selected.id === "openapi" ? <TextInput accessibilityLabel="Endpoint" placeholder="https://service.example.com/api" placeholderTextColor={colors.textTertiary} autoCapitalize="none" autoCorrect={false} value={endpoint} onChangeText={setEndpoint} style={inputStyle} /> : null}
          {!browserAuth ? <TextInput accessibilityLabel="Account token" placeholder={selected.id === "github" || selected.id === "notion" || selected.id === "slack" || selected.id === "linear" ? "Account token" : "Account token (optional)"} placeholderTextColor={colors.textTertiary} secureTextEntry autoCapitalize="none" autoCorrect={false} autoComplete="off" value={credential} onChangeText={setCredential} style={inputStyle} /> : null}
          {selected.id === "openapi" ? <TextInput accessibilityLabel="OpenAPI document" placeholder="OpenAPI 3.0 JSON document" placeholderTextColor={colors.textTertiary} multiline value={spec} onChangeText={setSpec} style={[inputStyle, { minHeight: 140, textAlignVertical: "top" }]} /> : null}
          {oauthSupported ? <>
            {browserAuth ? <ListRow title="Allow updates" subtitle="Request write access. Each write tool still starts disabled." trailing={<Switch accessibilityLabel="Request write access" value={allowWrites} onValueChange={setAllowWrites} />} /> : null}
            {selected.id !== "google" ? <Button label={tokenMode ? "Use browser authorization" : "Use token or public endpoint"} variant="plain" onPress={() => { setTokenMode((value) => !value); setCredential(""); }} /> : null}
            {browserAuth && (!oauthConfigured.includes(selected.id) || configuring) ? <ListSection title="Server authorization setup" footer={selected.id === "google" ? "Use your Google web application OAuth client. Register this server’s HTTPS callback in Google Cloud." : "Set this server’s HTTPS callback once. Providers supporting dynamic registration do not need a client ID."}>
              <TextInput accessibilityLabel="OAuth callback URL" placeholder="https://your-server/plugins/oauth/callback" placeholderTextColor={colors.textTertiary} autoCapitalize="none" autoCorrect={false} value={callbackUrl} onChangeText={setCallbackUrl} style={inputStyle} />
              <TextInput accessibilityLabel="OAuth client ID" placeholder={selected.id === "google" ? "Client ID" : "Client ID (if registered)"} placeholderTextColor={colors.textTertiary} autoCapitalize="none" autoCorrect={false} value={clientId} onChangeText={setClientId} style={inputStyle} />
              <TextInput accessibilityLabel="OAuth client secret" placeholder="Client secret (if required)" placeholderTextColor={colors.textTertiary} secureTextEntry autoCapitalize="none" autoCorrect={false} value={clientSecret} onChangeText={setClientSecret} style={inputStyle} />
              <Button label="Save authorization setup" loading={busy} disabled={!callbackUrl.trim()} onPress={() => void saveOAuthConfig()} />
            </ListSection> : browserAuth ? <Button label="Edit authorization setup" variant="plain" onPress={() => setConfiguring(true)} /> : null}
          </> : null}
          {selected.id === "mcp" || selected.id === "openapi" ? <>
            <ListRow title="Trust an internal endpoint" subtitle="Allow this account to reach specified LAN, loopback or Tailscale addresses." trailing={<Switch accessibilityLabel="Trust internal endpoint" value={trustInternal} onValueChange={setTrustInternal} />} />
            {trustInternal ? <TextInput accessibilityLabel="Trusted internal ranges" placeholder="192.168.1.10/32, 100.64.1.2/32" placeholderTextColor={colors.textTertiary} autoCapitalize="none" autoCorrect={false} value={networks} onChangeText={setNetworks} style={inputStyle} /> : null}
          </> : null}
          <AppText variant="caption" tone="secondary">Credentials stay on this server. {selected.id === "notion" ? "Share the pages you want this account to access in Notion." : "Only enabled tools are available to agents."}</AppText>
          <Button label={browserAuth ? "Continue in browser" : "Connect account"} loading={busy} disabled={!name.trim() || (browserAuth && !oauthConfigured.includes(selected.id)) || (trustInternal && !networks.trim())} onPress={() => void connect()} />
          <Button label="Account setup" variant="plain" onPress={() => void Linking.openURL(selected.setup_url)} />
          <Button label="Cancel" variant="plain" disabled={busy} onPress={clearForm} />
        </View>}
        {account ? <>
          <ListSection title={account.name} footer={account.verified_at ? `Last verified ${new Date(account.verified_at).toLocaleString()}` : "Authorization has not been verified."}>
            <ListRow title={account.identity} value={accountStatus(account)} numberOfLines={2} />
            {account.trusted_networks?.length ? <ListRow title="Trusted internal ranges" subtitle={account.trusted_networks.join(", ")} /> : null}
            <ListRow title="Enabled" trailing={<Switch accessibilityLabel="Enable account" value={account.enabled} disabled={busy || account.status === "disconnected"} onValueChange={(enabled) => void request({ action: enabled ? "enable" : "disable", id: account.id })} />} />
            <ListRow title="Refresh tools and status" icon="refresh-outline" disabled={busy || account.status === "disconnected"} onPress={() => void request({ action: "refresh", id: account.id })} />
          </ListSection>
          <ListSection title="Available tools" footer="Enable only the tools you want Brain and Workers to use with this account.">
            {(account.tools ?? []).map((tool) => <View key={tool.name}>
              <ListRow title={tool.name} subtitle={tool.description} numberOfLines={3} trailing={<Switch accessibilityLabel={`Allow ${tool.name}`} value={tool.allowed} disabled={busy || account.status === "disconnected"} onValueChange={(allowed) => changePolicy(tool.name, allowed)} />} />
              <ToolSchema schema={tool.input_schema} />
            </View>)}
          </ListSection>
          <ListSection title="Recent calls">
            {(account.history ?? []).length ? account.history.map((event, index) => <ListRow key={`${event.at}_${index}`} title={event.tool} subtitle={event.message || new Date(event.at).toLocaleString()} value={event.status === "success" ? "Success" : "Error"} numberOfLines={3} />) : <ListRow title="No calls yet" subtitle="Results appear here after a tool is used." />}
          </ListSection>
          {account.status !== "disconnected" || account.credential_removal_pending ? <Button label={account.credential_removal_pending ? "Retry credential removal" : "Disconnect account"} variant="destructive" disabled={busy} onPress={() => Alert.alert("Disconnect account?", "This stops future calls, revokes OAuth access where supported, and removes the stored credential.", [{ text: "Cancel", style: "cancel" }, { text: "Disconnect", style: "destructive", onPress: () => { if (valid()) void request({ action: "disconnect", id: account.id }); } }])} /> : null}
        </> : null}
      </> : null}
    </ScrollView>
  );
}

function ToolSchema({ schema }: { schema?: Record<string, unknown> }) {
  const [expanded, setExpanded] = useState(false);
  return <View style={styles.schema}>
    <Button label={expanded ? "Hide inputs" : "Inspect inputs"} variant="plain" size="sm" onPress={() => setExpanded((value) => !value)} />
    {expanded ? <AppText variant="caption" tone="secondary" selectable>{JSON.stringify(schema, null, 2)}</AppText> : null}
  </View>;
}
const styles = StyleSheet.create({
  content: { padding: 20, paddingBottom: 48, gap: 16 },
  form: { gap: 12 },
  input: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 12, padding: 14, minHeight: 48, fontSize: 16 },
  schema: { paddingHorizontal: 12, paddingBottom: 8, alignItems: "flex-start" },
});
