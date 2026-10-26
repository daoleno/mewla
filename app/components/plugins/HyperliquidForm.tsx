import React, { useState } from "react";
import { Linking, StyleSheet, View } from "react-native";
import { Field } from "./CustomServiceForm";
import { Button, ListRow, ListSection } from "../ui";
import type { ConnectionRequest, PluginIntegration } from "../../services/connections";

const API_WALLETS = "https://app.hyperliquid.xyz/API";

/**
 * Trading, apart from reading: an optional step on the service page that stays
 * one row until the person wants it. The key names its own account and
 * network, so nothing else is asked.
 */
export function HyperliquidForm({ plugin, connecting, serverName, defaultOpen = false, onConnect }: {
  plugin: PluginIntegration; connecting: boolean; serverName: string; defaultOpen?: boolean;
  onConnect: (input: NonNullable<ConnectionRequest["input"]>, signIn: boolean) => Promise<void>;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const [key, setKey] = useState("");
  const connect = async () => {
    const credential = key.trim();
    setKey("");
    await onConnect({ integration: plugin.id, credential }, false);
  };
  if (!open) {
    return <ListSection title="Trading">
      <ListRow icon="key" title="Add API wallet key" accessory="chevron" onPress={() => setOpen(true)} />
    </ListSection>;
  }
  return <View style={styles.form}>
    <ListSection title="Trading" footer={`The key stays on ${serverName}.`}>
      <Field label="API wallet key" accessibilityLabel="Hyperliquid API wallet key" placeholder="0x…" secureTextEntry autoCapitalize="none" autoCorrect={false} value={key} onChangeText={setKey} />
    </ListSection>
    <Button label="Add key" variant="filled" size="lg" block loading={connecting} disabled={!key.trim()} onPress={() => void connect()} />
    <Button label="Create an API wallet" icon="open-external" variant="plain" block onPress={() => void Linking.openURL(API_WALLETS)} />
  </View>;
}

const styles = StyleSheet.create({
  form: { gap: 8 },
});
