import React, { useState } from "react";
import { StyleSheet, Switch, TextInput, View } from "react-native";
import { AppText, Button, ListRow } from "../ui";
import { useAppColors } from "../../constants/tokens";
import type { ConnectionRequest, ConnectionResponse, PluginIntegration } from "../../services/connections";

export function CustomServiceForm({ plugin, busy, send, authorize }: {
  plugin: PluginIntegration; busy: boolean;
  send: (request: ConnectionRequest) => Promise<ConnectionResponse | undefined>;
  authorize: (input: NonNullable<ConnectionRequest["input"]>) => Promise<void>;
}) {
  const colors = useAppColors();
  const [name, setName] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [token, setToken] = useState("");
  const [spec, setSpec] = useState("");
  const [browser, setBrowser] = useState(plugin.id === "mcp");
  const [trust, setTrust] = useState(false);
  const [networks, setNetworks] = useState("");
  const [error, setError] = useState("");
  const inputStyle = [styles.input, { color: colors.textPrimary, backgroundColor: colors.bgSurface, borderColor: colors.border }];
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
  return <View style={styles.form}>
    <AppText tone="secondary">For services you or your organization operate. You’ll need their endpoint and authorization requirements.</AppText>
    <TextInput accessibilityLabel="Service name" placeholder="Service name (optional)" value={name} onChangeText={setName} style={inputStyle} placeholderTextColor={colors.textTertiary} />
    <TextInput accessibilityLabel="Service endpoint" placeholder="https://service.example.com/mcp" value={endpoint} onChangeText={setEndpoint} autoCapitalize="none" autoCorrect={false} style={inputStyle} placeholderTextColor={colors.textTertiary} />
    {plugin.id === "mcp" ? <ListRow title="Sign in with browser" subtitle="Register automatically when supported. Registered clients are managed by the server owner." trailing={<Switch accessibilityLabel="Sign in with browser" value={browser} onValueChange={setBrowser} />} /> : null}
    {!browser ? <TextInput accessibilityLabel="Service token" placeholder="Token (optional for public services)" secureTextEntry autoCapitalize="none" autoCorrect={false} value={token} onChangeText={setToken} style={inputStyle} placeholderTextColor={colors.textTertiary} /> : null}
    {plugin.id === "openapi" ? <TextInput accessibilityLabel="OpenAPI document" placeholder="OpenAPI 3 JSON document" multiline value={spec} onChangeText={setSpec} style={[inputStyle, { minHeight: 140, textAlignVertical: "top" }]} placeholderTextColor={colors.textTertiary} /> : null}
    <ListRow title="Allow a private network" subtitle="Only for an internal service you trust." trailing={<Switch accessibilityLabel="Allow a private network" value={trust} onValueChange={setTrust} />} />
    {trust ? <TextInput accessibilityLabel="Trusted network ranges" placeholder="192.168.1.10/32" value={networks} onChangeText={setNetworks} autoCapitalize="none" autoCorrect={false} style={inputStyle} placeholderTextColor={colors.textTertiary} /> : null}
    <AppText variant="caption" tone="secondary">Custom tools need individual permission after connection. Credentials stay on the current server.</AppText>
    {error ? <AppText accessibilityRole="alert" style={{ color: colors.dangerText }}>{error}</AppText> : null}
    <Button label={browser ? "Continue in browser" : "Connect service"} loading={busy} disabled={!endpoint.trim() || trust && !networks.trim()} onPress={() => void connect()} />
  </View>;
}
const styles = StyleSheet.create({ form: { gap: 16 }, input: { borderWidth: 1, borderRadius: 12, minHeight: 48, padding: 14, fontSize: 16 } });
