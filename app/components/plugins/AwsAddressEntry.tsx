import React, { useEffect, useRef, useState } from "react";
import { AppState } from "react-native";
import * as Clipboard from "expo-clipboard";
import { Field } from "./CustomServiceForm";
import { Button } from "../ui";
import { signInAddressFromText } from "../../services/pluginConnectionsModel";

/**
 * The last step of a sign-in that returns to the server's own address, when the
 * browser is on another device and the page did not load. It fills from the
 * clipboard when the person comes back from the browser, so the usual path is
 * copy, return, Continue. Typing replaces it.
 */
export function AwsAddressEntry({ onSubmit }: { onSubmit(address: string): void }) {
  const [address, setAddress] = useState("");
  const typed = useRef(false);
  useEffect(() => {
    let alive = true;
    const fill = async () => {
      try {
        const found = signInAddressFromText(await Clipboard.getStringAsync());
        if (alive && found && !typed.current) setAddress(found);
      } catch { /* The clipboard can be off limits; the field is still there. */ }
    };
    void fill();
    const subscription = AppState.addEventListener("change", (state) => { if (state === "active") void fill(); });
    return () => { alive = false; subscription.remove(); };
  }, []);
  const ready = signInAddressFromText(address) !== "";
  return <>
    <Field
      label="Address from the browser"
      accessibilityLabel="Address the sign-in ended on"
      testID="sign-in-address"
      autoCapitalize="none"
      autoCorrect={false}
      autoComplete="off"
      keyboardType="url"
      value={address}
      onChangeText={(text) => { typed.current = true; setAddress(text); }}
      onSubmitEditing={() => { if (ready) onSubmit(address.trim()); }}
      returnKeyType="done"
    />
    <Button label="Continue" variant="filled" block disabled={!ready} onPress={() => onSubmit(address.trim())} testID="sign-in-address-continue" />
  </>;
}
