import React, { useRef, useState } from "react";
import { StyleSheet, View, type TextInputInstance } from "react-native";
import * as Clipboard from "expo-clipboard";
import { AppText, Button, InlineNotice } from "../ui";
import { MultilineField } from "../ui/MultilineField";


/** The vendor CLI owns the remote sign-in; codes are sent only on Continue. */
export function CloudCodeEntry({ onSubmit, serviceName = "AWS", noun = "code" }: { onSubmit(code: string): Promise<void>; serviceName?: string; /** What the person pastes: a sign-in code, or a client secret. */ noun?: string }) {
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
    catch (failure) { setError(failure instanceof Error ? failure.message : `Could not send the ${noun}. Try again.`); }
    finally { submitting.current = false; setBusy(false); }
  };
  const paste = async () => {
    try {
      const text = await Clipboard.getStringAsync();
      setCode(text); setError(text.trim() ? "" : `The clipboard is empty. Copy the ${noun} first.`);
    } catch {
      setError("Clipboard access is unavailable. Paste directly into the code field.");
    }
    input.current?.focus();
  };
  return <>
    <View style={styles.field}>
      <AppText variant="caption" tone="secondary">{noun === "code" ? `Paste the code ${serviceName} shows` : `Paste the ${noun}`}</AppText>
      <MultilineField
        ref={input}
        minHeight={48}
        accessibilityLabel={noun === "code" ? `Paste the code ${serviceName} shows` : `Paste the ${noun}`}
        testID="sign-in-code"
        placeholder={`Paste ${noun} here`}
        autoCapitalize="none"
        autoCorrect={false}
        autoComplete="off"
        editable={!busy}
        value={code}
        onChangeText={(text) => { setCode(text); setError(""); }}
        onSubmitEditing={() => void submit()}
        returnKeyType="done"
      />
    </View>
    {error ? <InlineNotice tone="danger" title={`Check the ${noun}`} detail={error} detailNumberOfLines={0} /> : null}
    <Button label={`Paste ${noun}`} variant="tinted" block disabled={busy} onPress={() => void paste()} />
    <Button label="Continue" variant="filled" block loading={busy} disabled={!code.trim()} onPress={() => void submit()} testID="sign-in-code-continue" />
  </>;
}

const styles = StyleSheet.create({
  field: { gap: 8 },
});
