import React, { useState } from "react";
import { StyleSheet, Switch, TextInput, View, type TextInputProps } from "react-native";
import { AppText, Button, InlineNotice, ListRow, ListSection } from "../ui";
import { useAppColors } from "../../constants/tokens";
import type { ConnectionRequest, ConnectionResponse, PluginIntegration } from "../../services/connections";

export function CustomServiceForm({ plugin, busy, serverName, send, authorize }: {
  plugin: PluginIntegration; busy: boolean; serverName: string;
  send: (request: ConnectionRequest) => Promise<ConnectionResponse | undefined>;
  authorize: (input: NonNullable<ConnectionRequest["input"]>) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [token, setToken] = useState("");
  const [spec, setSpec] = useState("");
  const [browser, setBrowser] = useState(plugin.id === "mcp");
  const [trust, setTrust] = useState(false);
  const [networks, setNetworks] = useState("");
  const [error, setError] = useState("");
  const connect = async () => {
    setError("");
    let document: unknown;
    if (plugin.id === "openapi") {
      try { document = JSON.parse(spec); } catch { setError("Enter a valid OpenAPI JSON document."); return; }
    }
    const input = { integration: plugin.id, name: name.trim() || plugin.name, endpoint: endpoint.trim(), credential: token, spec: document, trusted_networks: trust ? networks.split(",").map((s) => s.trim()).filter(Boolean) : undefined };
    setToken("");
    if (browser) await authorize(input);
    else await send({ action: "add", input });
  };
  return <View>
    <ListSection title="Service">
      <Field label="Name" accessibilityLabel="Service name" placeholder={plugin.name} value={name} onChangeText={setName} />
      <Field label="Endpoint" accessibilityLabel="Service endpoint" placeholder="https://service.example.com/mcp" value={endpoint} onChangeText={setEndpoint} autoCapitalize="none" autoCorrect={false} keyboardType="url" />
    </ListSection>
    <ListSection title="Authorization">
      {plugin.id === "mcp" ? <ListRow icon="open-outline" title="Sign in with browser" subtitle="Registers automatically when supported." numberOfLines={2} trailing={<Switch accessibilityLabel="Sign in with browser" value={browser} onValueChange={setBrowser} />} /> : null}
      {!browser ? <Field label="Token" accessibilityLabel="Service token" placeholder="Optional for public services" secureTextEntry autoCapitalize="none" autoCorrect={false} value={token} onChangeText={setToken} /> : null}
    </ListSection>
    {plugin.id === "openapi" ? <ListSection title="OpenAPI document">
      <Field label="OpenAPI 3 JSON" accessibilityLabel="OpenAPI document" placeholder="Paste the JSON document" multiline value={spec} onChangeText={setSpec} />
    </ListSection> : null}
    <ListSection title="Network" footer="Only for an internal service you trust.">
      <ListRow icon="lock-open-outline" title="Allow a private network" trailing={<Switch accessibilityLabel="Allow a private network" value={trust} onValueChange={setTrust} />} />
      {trust ? <Field label="Trusted ranges" accessibilityLabel="Trusted network ranges" placeholder="192.168.1.10/32" value={networks} onChangeText={setNetworks} autoCapitalize="none" autoCorrect={false} /> : null}
    </ListSection>
    {error ? <InlineNotice tone="danger" title="Check the form" detail={error} style={styles.error} /> : null}
    <Button label={browser ? "Continue in browser" : "Connect service"} variant="filled" size="lg" block loading={busy} disabled={!endpoint.trim() || trust && !networks.trim()} onPress={() => void connect()} />
    <AppText variant="caption" tone="tertiary" style={styles.note}>Custom tools need individual permission after connection. Credentials stay on {serverName}.</AppText>
  </View>;
}

function Field({ label, multiline, ...props }: TextInputProps & { label: string }) {
  const colors = useAppColors();
  return <View style={styles.field}>
    <AppText variant="caption" tone="tertiary">{label}</AppText>
    <TextInput {...props} multiline={multiline} placeholderTextColor={colors.textTertiary} style={[styles.input, { color: colors.textPrimary }, multiline && styles.multiline]} />
  </View>;
}

const styles = StyleSheet.create({
  field: { paddingHorizontal: 16, paddingTop: 10, paddingBottom: 4 },
  input: { minHeight: 44, fontSize: 16, paddingVertical: 8, paddingHorizontal: 0 },
  multiline: { minHeight: 140, textAlignVertical: "top" },
  error: { marginBottom: 16 },
  note: { paddingHorizontal: 16, paddingTop: 10 },
});
