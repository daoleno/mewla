import React, { useRef, useState } from "react";
import { StyleSheet, View } from "react-native";
import type { PluginAccount } from "../../services/connections";
import { useAppTheme } from "../../constants/tokens";
import { AppText, Button } from "../ui";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";

export function suggestedAWSProfile(account: PluginAccount): string {
  const accountName = account.aws?.account_name || account.aws?.account_id || "aws";
  const role = (account.aws?.who || "").replace(/^Role\s+/i, "").replace(/^IAM user\s+/i, "");
  return [accountName, role].filter(Boolean).join("-").toLowerCase().replace(/[^a-z0-9_.-]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 128);
}
export function CloudProfileNameEntry({ account, suggest = false, onSave }: {
  account: PluginAccount;
  suggest?: boolean;
  onSave(name: string): Promise<boolean>;
}) {
  const { colors } = useAppTheme();
  const [name, setName] = useState(() => suggest ? suggestedAWSProfile(account) : account.profile || account.name);
  const saving = useRef(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const save = async () => {
    if (saving.current) return;
    if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(name.trim())) { setError("Use letters, numbers, dots, underscores or hyphens."); return; }
    saving.current = true; setBusy(true); setError("");
    try { if (!await onSave(name.trim())) setError("Couldn't rename this profile. Check the name and try again."); }
    catch (failure) { setError(failure instanceof Error ? failure.message : "Couldn't rename this profile. Try again."); }
    finally { saving.current = false; setBusy(false); }
  };
  return <View style={styles.form}>
    <AppText variant="compact">Profile name</AppText>
    <MobileSingleLineInput framed value={name} onChangeText={setName} accessibilityLabel="Profile name" placeholderTextColor={colors.textTertiary} autoCapitalize="none" autoCorrect={false} maxLength={128} editable={!busy} onSubmitEditing={() => void save()} />
    {error ? <AppText variant="caption" accessibilityLiveRegion="polite">{error}</AppText> : null}
    <Button label="Save name" variant="tinted" loading={busy} onPress={() => void save()} />
  </View>;
}
const styles = StyleSheet.create({ form: { padding: 14, gap: 10 } });
