import React, { useState } from "react";
import { Linking, StyleSheet, View } from "react-native";
import { Field } from "./CustomServiceForm";
import { AppText, Button, ListRow, ListSection } from "../ui";
import type { ConnectionRequest, PluginAccount, PluginIntegration } from "../../services/connections";

type Job = { read: string; write: string; example: string } | undefined;

/**
 * A service reached with a pasted API key: what Brain can do with it, and
 * the key card. Image generation also takes its endpoint and default model.
 */
export function ApiKeyForm({ plugin, job, serverName, connecting, onConnect }: {
  plugin: PluginIntegration; job: Job; serverName: string; connecting: boolean;
  onConnect: (input: NonNullable<ConnectionRequest["input"]>) => Promise<void>;
}) {
  const image = plugin.id === "image";
  const [name, setName] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [model, setModel] = useState("");
  const [key, setKey] = useState("");
  const ready = key.trim() && (!image || endpoint.trim() && model.trim());
  const connect = async () => {
    const input = { integration: plugin.id, name: name.trim() || plugin.name, endpoint: image ? endpoint.trim() : undefined, model: image ? model.trim() : undefined, credential: key.trim() };
    setKey("");
    await onConnect(input);
  };
  return <View>
    <ListSection title="What Brain can do" footer="Generation costs money, so it waits until you allow changes.">
      {job?.read ? <ListRow icon="eye" title="Read and search" subtitle={job.read} numberOfLines={3} value="On" /> : null}
      <ListRow icon="edit" title="Make changes when asked" subtitle={job ? `${job.write}. Off until you allow it.` : "Off until you allow it."} numberOfLines={3} value="Off" />
    </ListSection>
    {image ? <ListSection title="Endpoint" footer="Any OpenAI-compatible images endpoint: the address before /images/generations.">
      <Field label="Name" accessibilityLabel="Account name" placeholder={plugin.name} value={name} onChangeText={setName} />
      <Field label="Base URL" accessibilityLabel="Base URL" placeholder="https://images.example.com/v1" value={endpoint} onChangeText={setEndpoint} autoCapitalize="none" autoCorrect={false} keyboardType="url" />
      <Field label="Default model" accessibilityLabel="Default model" placeholder="Model name" value={model} onChangeText={setModel} autoCapitalize="none" autoCorrect={false} />
    </ListSection> : null}
    <ListSection title="API key" footer={`The key stays on ${serverName} and is never shown again.`}>
      <Field label="API key" accessibilityLabel={`${plugin.name} API key`} placeholder="Paste the key" secureTextEntry autoCapitalize="none" autoCorrect={false} value={key} onChangeText={setKey} />
      {plugin.setup_url ? <ListRow icon="open-external" title={`Get a ${plugin.name} key`} onPress={() => void Linking.openURL(plugin.setup_url)} /> : null}
    </ListSection>
    <Button label={`Connect ${plugin.name}`} variant="filled" size="lg" block loading={connecting} disabled={!ready} onPress={() => void connect()} />
  </View>;
}

/** Replaces the key of an account whose key is missing or was rejected. */
export function AccountKeyEntry({ account, serviceName, saving, disabled, onSave }: {
  account: PluginAccount; serviceName: string; saving: boolean; disabled: boolean; onSave(key: string): void;
}) {
  const [key, setKey] = useState("");
  return <View style={styles.entry}>
    <Field label="API key" accessibilityLabel={`${serviceName} API key for ${account.name}`} placeholder="Paste the key" secureTextEntry autoCapitalize="none" autoCorrect={false} value={key} onChangeText={setKey} />
    <View style={styles.actions}>
      <AppText variant="caption" tone="tertiary" style={styles.note}>Checked with {serviceName} before it is saved.</AppText>
      <Button label="Save key" size="sm" variant="tinted" loading={saving} disabled={disabled || !key.trim()} onPress={() => { onSave(key.trim()); setKey(""); }} />
    </View>
  </View>;
}

const styles = StyleSheet.create({
  entry: { paddingBottom: 10 },
  actions: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 12, paddingHorizontal: 16 },
  note: { flexShrink: 1 },
});
