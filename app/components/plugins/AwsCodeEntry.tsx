import React, { useEffect, useRef, useState } from "react";
import { AppState } from "react-native";
import * as Clipboard from "expo-clipboard";
import { Field } from "./CustomServiceForm";
import { Button } from "../ui";
import { awsCodeFromText } from "../../services/pluginConnectionsModel";

/**
 * The last step of AWS's console sign-in: the code AWS shows after signing in.
 * It fills from the clipboard when the person comes back from the browser, so
 * the usual path is copy, return, Continue. Typing replaces it.
 */
export function AwsCodeEntry({ onSubmit }: { onSubmit(code: string): void }) {
  const [code, setCode] = useState("");
  const typed = useRef(false);
  useEffect(() => {
    let alive = true;
    const fill = async () => {
      try {
        const found = awsCodeFromText(await Clipboard.getStringAsync());
        if (alive && found && !typed.current) setCode(found);
      } catch { /* The clipboard can be off limits; the field is still there. */ }
    };
    void fill();
    const subscription = AppState.addEventListener("change", (state) => { if (state === "active") void fill(); });
    return () => { alive = false; subscription.remove(); };
  }, []);
  const ready = awsCodeFromText(code) !== "";
  return <>
    <Field
      label="Code from AWS"
      accessibilityLabel="AWS authorization code"
      testID="aws-authorization-code"
      autoCapitalize="none"
      autoCorrect={false}
      autoComplete="off"
      value={code}
      onChangeText={(text) => { typed.current = true; setCode(text); }}
      onSubmitEditing={() => { if (ready) onSubmit(code.trim()); }}
      returnKeyType="done"
    />
    <Button label="Continue" variant="filled" block disabled={!ready} onPress={() => onSubmit(code.trim())} testID="aws-authorization-continue" />
  </>;
}
