import React, { useRef, useState } from "react";
import { StyleSheet, TextInput, View, type TextInputInstance } from "react-native";
import * as Clipboard from "expo-clipboard";
import { Radii, useAppColors } from "../../constants/tokens";
import { AppText, Button, InlineNotice } from "../ui";
import { outlinedSurface } from "../ui/outlinedSurface";


/** The vendor CLI owns the remote sign-in; codes are sent only on Continue. */
export function CloudCodeEntry({ onSubmit, serviceName = "AWS" }: { onSubmit(code: string): Promise<void>; serviceName?: string }) {
  const colors = useAppColors();
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const input = useRef<TextInputInstance>(null);
  const submit = async () => {
    if (submitting.current) return;
    const callback = code.trim();
    if (!callback) return;
    submitting.current = true; setBusy(true); setError("");
    try { await onSubmit(callback); }
    catch (failure) { setError(failure instanceof Error ? failure.message : "Could not send the code. Try again."); }
    finally { submitting.current = false; setBusy(false); }
  };
  const paste = async () => {
    try {
      const text = await Clipboard.getStringAsync();
      setCode(text); setError(text.trim() ? "" : "The clipboard is empty. Copy the code from the browser first.");
    } catch {
      setError("Clipboard access is unavailable. Paste directly into the code field.");
    }
    input.current?.focus();
  };
  return <>
    <View style={styles.field}>
      <AppText variant="caption" tone="secondary">{`Paste the code ${serviceName} shows`}</AppText>
      <TextInput
        ref={input}
        accessibilityLabel={`Paste the code ${serviceName} shows`}
        testID="sign-in-code"
        placeholder="Paste code here"
        placeholderTextColor={colors.textTertiary}
        style={[styles.input, { color: colors.textPrimary, backgroundColor: colors.bgElevated, borderColor: colors.border }]}
        autoCapitalize="none"
        autoCorrect={false}
        autoComplete="off"
        multiline
        editable={!busy}
        value={code}
        onChangeText={(text) => { setCode(text); setError(""); }}
        onSubmitEditing={() => void submit()}
        returnKeyType="done"
      />
    </View>
    {error ? <InlineNotice tone="danger" title="Check the code" detail={error} detailNumberOfLines={0} /> : null}
    <Button label="Paste code" variant="tinted" block disabled={busy} onPress={() => void paste()} />
    <Button label="Continue" variant="filled" block loading={busy} disabled={!code.trim()} onPress={() => void submit()} testID="sign-in-code-continue" />
  </>;
}

const styles = StyleSheet.create({
  field: { gap: 8 },
  input: { ...outlinedSurface(Radii.md), minHeight: 48, fontSize: 16, paddingHorizontal: 12, paddingVertical: 12 },
});
