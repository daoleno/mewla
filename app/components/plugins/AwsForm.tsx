import React, { useEffect, useState } from "react";
import { StyleSheet, View } from "react-native";
import { Field } from "./CustomServiceForm";
import { AppText, Button, InlineNotice, ListRow, ListSection } from "../ui";
import { awsKeysCredential, awsProfileSignsInWithBrowser, awsProfileSubtitle } from "../../services/pluginConnectionsModel";
import type { AwsProfile, ConnectionRequest, PluginAccount, PluginIntegration } from "../../services/connections";

type Input = NonNullable<ConnectionRequest["input"]>;

/**
 * How AWS connects: one Sign in to AWS button (AWS's own page in the browser;
 * the server's AWS CLI keeps the session and Mewla no key), a profile of that
 * CLI that is already signed in, or pasted access keys, which stay in the
 * server's vault. Profiles are read from that computer when the form opens.
 */
export function AwsForm({ plugin, accounts, profiles, connecting, serverName, onLoad, onConnect }: {
  plugin: PluginIntegration; accounts: readonly PluginAccount[]; profiles: AwsProfile[] | null;
  connecting: boolean; serverName: string;
  onLoad: () => Promise<void>;
  onConnect: (input: Input, signIn: boolean) => Promise<void>;
}) {
  const [keys, setKeys] = useState(false);
  const [accessKeyId, setAccessKeyId] = useState("");
  const [secretAccessKey, setSecretAccessKey] = useState("");
  const [sessionToken, setSessionToken] = useState("");
  const [region, setRegion] = useState("");
  useEffect(() => { void onLoad(); }, []);
  const connected = new Set(accounts.filter((account) => account.status !== "disconnected").map((account) => account.profile));
  const connectKeys = async () => {
    const credential = awsKeysCredential({ accessKeyId, secretAccessKey, sessionToken });
    setAccessKeyId(""); setSecretAccessKey(""); setSessionToken("");
    await onConnect({ integration: plugin.id, credential, region: region.trim() || undefined }, false);
  };
  return <View style={styles.form}>
    <Button label="Sign in to AWS" icon="open-external" variant="filled" size="lg" block loading={connecting} onPress={() => void onConnect({ integration: plugin.id }, true)} />
    {profiles?.length ? <ListSection title={`Already on ${serverName}`}>
      {profiles.map((profile) => <ListRow
        key={profile.name}
        icon="person-circle"
        title={profile.name}
        subtitle={[awsProfileSubtitle(profile), connected.has(profile.name) ? "Connected" : ""].filter(Boolean).join(" · ")}
        numberOfLines={2}
        accessory="chevron"
        disabled={connecting}
        // A browser profile connects at once while signed in, and signs in again in place once expired; keys and roles just connect.
        onPress={() => void onConnect({ integration: plugin.id, profile: profile.name }, awsProfileSignsInWithBrowser(profile))}
      />)}
    </ListSection> : null}
    {keys ? <>
      <ListSection title="Access keys" footer={`The keys stay on ${serverName}.`}>
        <Field label="Access key ID" accessibilityLabel="AWS access key ID" placeholder="AKIA…" autoCapitalize="characters" autoCorrect={false} value={accessKeyId} onChangeText={setAccessKeyId} />
        <Field label="Secret access key" accessibilityLabel="AWS secret access key" secureTextEntry autoCapitalize="none" autoCorrect={false} value={secretAccessKey} onChangeText={setSecretAccessKey} />
        <Field label="Session token (temporary keys only)" accessibilityLabel="AWS session token" secureTextEntry autoCapitalize="none" autoCorrect={false} value={sessionToken} onChangeText={setSessionToken} />
        <Field label="Region" accessibilityLabel="AWS region" placeholder="us-east-1" autoCapitalize="none" autoCorrect={false} value={region} onChangeText={setRegion} />
      </ListSection>
      <Button label="Connect keys" variant="filled" size="lg" block loading={connecting} disabled={!accessKeyId.trim() || !secretAccessKey.trim()} onPress={() => void connectKeys()} />
    </> : <ListSection>
      <ListRow icon="key" title="Paste access keys" accessory="chevron" onPress={() => setKeys(true)} />
    </ListSection>}
    <AppText variant="caption" tone="tertiary" style={styles.note}>Each change Brain makes in AWS needs your permission first.</AppText>
  </View>;
}

const styles = StyleSheet.create({
  form: { gap: 8 },
  note: { paddingHorizontal: 16 },
});
