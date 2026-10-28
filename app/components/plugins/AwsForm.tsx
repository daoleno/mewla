import React, { useEffect, useState } from "react";
import { StyleSheet, View } from "react-native";
import { Field } from "./CustomServiceForm";
import { AppText, Button, InlineNotice, ListRow, ListSection } from "../ui";
import { awsKeysCredential, awsProfileSubtitle } from "../../services/pluginConnectionsModel";
import type { AwsProfile, ConnectionRequest, PluginAccount, PluginIntegration } from "../../services/connections";

type Input = NonNullable<ConnectionRequest["input"]>;

/**
 * How AWS connects: pick a profile of the AWS CLI on the server's computer
 * (Mewla keeps no key for it), or paste access keys, which stay in the
 * server's vault. Profiles are read from that computer when the form opens.
 */
export function AwsForm({ plugin, accounts, profiles, loading, connecting, serverName, onLoad, onConnect }: {
  plugin: PluginIntegration; accounts: readonly PluginAccount[]; profiles: AwsProfile[] | null; loading: boolean;
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
    <ListSection
      title={`Profiles on ${serverName}`}
      footer="Mewla uses the AWS CLI’s own sign-in and keeps no key for a profile. If a profile has expired, run aws login --profile NAME on that computer first."
    >
      {profiles === null ? <ListRow icon="refresh" title={loading ? "Looking for profiles…" : "Profiles not loaded"} disabled /> : profiles.length === 0 ? (
        <ListRow icon="info" title="No AWS profiles found" subtitle="Run aws login on that computer, then look again." numberOfLines={3} />
      ) : profiles.map((profile) => <ListRow
        key={profile.name}
        icon="person-circle"
        title={profile.name}
        subtitle={[awsProfileSubtitle(profile), connected.has(profile.name) ? "Connected" : ""].filter(Boolean).join(" · ")}
        numberOfLines={2}
        accessory="chevron"
        disabled={connecting}
        onPress={() => void onConnect({ integration: plugin.id, profile: profile.name }, false)}
      />)}
      <ListRow icon="refresh" title="Look again" disabled={loading || connecting} onPress={() => void onLoad()} />
    </ListSection>
    {keys ? <>
      <ListSection title="Access keys" footer={`The keys stay on ${serverName}. Prefer a profile when you have one.`}>
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
