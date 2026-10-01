import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Linking, ScrollView, StyleSheet, Switch, TextInput, View } from "react-native";
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
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
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

  useEffect(() => {
    void request({ action: "list" });
    const onConnected = (event: { serverId: string }) => { if (event.serverId === serverId) void request({ action: "list" }); };
    wsClient.on("connected", onConnected);
    return () => wsClient.off("connected", onConnected);
  }, [request, serverId]);

  const clearForm = () => { setAdding(false); setCredential(""); setName(""); setEndpoint(""); setSpec(""); };
  const connect = async () => {
    if (!selected) return;
    let document: unknown;
    if (selected.id === "openapi") {
      try { document = JSON.parse(spec); } catch { setError("Enter a valid OpenAPI JSON document."); return; }
    }
    const secret = credential; setCredential("");
    const response = await request({ action: "add", input: { integration: selected.id, name: name.trim(), credential: secret, endpoint: endpoint.trim(), spec: document } });
    if (response && valid()) clearForm();
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
          {catalog.map((plugin) => <ListRow key={plugin.id} title={plugin.name} subtitle={plugin.description} value={plugin.available ? undefined : "Setup required"} accessory="chevron" onPress={() => { setSelected(plugin); setAccount(null); }} />)}
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
          <TextInput accessibilityLabel="Account token" placeholder={selected.id === "github" || selected.id === "notion" || selected.id === "slack" || selected.id === "linear" ? "Account token" : "Account token (optional)"} placeholderTextColor={colors.textTertiary} secureTextEntry autoCapitalize="none" autoCorrect={false} autoComplete="off" value={credential} onChangeText={setCredential} style={inputStyle} />
          {selected.id === "openapi" ? <TextInput accessibilityLabel="OpenAPI document" placeholder="OpenAPI 3.0 JSON document" placeholderTextColor={colors.textTertiary} multiline value={spec} onChangeText={setSpec} style={[inputStyle, { minHeight: 140, textAlignVertical: "top" }]} /> : null}
          <AppText variant="caption" tone="secondary">Credentials stay on this server. {selected.id === "notion" ? "Share the pages you want this account to access in Notion." : "Only enabled tools are available to agents."}</AppText>
          <Button label="Connect account" loading={busy} disabled={!name.trim()} onPress={() => void connect()} />
          <Button label="Account setup" variant="plain" onPress={() => void Linking.openURL(selected.setup_url)} />
          <Button label="Cancel" variant="plain" disabled={busy} onPress={clearForm} />
        </View>}
        {account ? <>
          <ListSection title={account.name} footer={account.verified_at ? `Last verified ${new Date(account.verified_at).toLocaleString()}` : "Authorization has not been verified."}>
            <ListRow title={account.identity} value={accountStatus(account)} numberOfLines={2} />
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
          <Button label="Disconnect account" variant="destructive" disabled={busy || account.status === "disconnected"} onPress={() => Alert.alert("Disconnect account?", "This removes its stored credential and stops future calls from Brain and Workers.", [{ text: "Cancel", style: "cancel" }, { text: "Disconnect", style: "destructive", onPress: () => { if (valid()) void request({ action: "disconnect", id: account.id }); } }])} />
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
