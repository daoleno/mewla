import React, { useRef, useState } from "react";
import { StyleSheet, TextInput, View, type TextInputInstance } from "react-native";
import * as Clipboard from "expo-clipboard";
import { Radii, useAppColors } from "../../constants/tokens";
import { AppText, Button, InlineNotice } from "../ui";
import { outlinedSurface } from "../ui/outlinedSurface";
import { signInAddressFromText } from "../../services/pluginConnectionsModel";

/** The loopback address copied from a browser on another computer. */
export function SignInAddressEntry({ onSubmit }: { onSubmit(address: string): Promise<void> }) {
  const colors = useAppColors();
  const [address, setAddress] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const input = useRef<TextInputInstance>(null);
  const submit = async () => {
    if (submitting.current) return;
    const callback = signInAddressFromText(address);
    if (!callback) {
      setError("Paste the full address starting with http://127.0.0.1 from the page opened after sign-in, even if that page could not load.");
      return;
    }
    submitting.current = true; setBusy(true); setError("");
    try { await onSubmit(callback); }
    catch (failure) { setError(failure instanceof Error ? failure.message : "Could not send the address. Try again."); }
    finally { submitting.current = false; setBusy(false); }
  };
  const paste = async () => {
    try {
      const text = await Clipboard.getStringAsync();
      setAddress(text); setError(text.trim() ? "" : "The clipboard is empty. Copy the address from the browser first.");
    } catch {
      setError("Clipboard access is unavailable. Paste directly into the address field.");
    }
    input.current?.focus();
  };
  return <>
    <View style={styles.field}>
      <AppText variant="caption" tone="secondary">Address from the browser</AppText>
      <TextInput
        ref={input}
        accessibilityLabel="Address the sign-in ended on"
        testID="sign-in-address"
        placeholder="http://127.0.0.1:…/plugins/oauth/callback?…"
        placeholderTextColor={colors.textTertiary}
        style={[styles.input, { color: colors.textPrimary, backgroundColor: colors.bgElevated, borderColor: colors.border }]}
        autoCapitalize="none"
        autoCorrect={false}
        autoComplete="off"
        keyboardType="url"
        editable={!busy}
        value={address}
        onChangeText={(text) => { setAddress(text); setError(""); }}
        onSubmitEditing={() => void submit()}
        returnKeyType="done"
      />
    </View>
    {error ? <InlineNotice tone="danger" title="Check the address" detail={error} /> : null}
    <Button label="Paste address" variant="tinted" block disabled={busy} onPress={() => void paste()} />
    <Button label="Continue" variant="filled" block loading={busy} disabled={!address.trim()} onPress={() => void submit()} testID="sign-in-address-continue" />
  </>;
}

const styles = StyleSheet.create({
  field: { gap: 8 },
  input: { ...outlinedSurface(Radii.md), minHeight: 48, fontSize: 16, paddingHorizontal: 12, paddingVertical: 12 },
});
