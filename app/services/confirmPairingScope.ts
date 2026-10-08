import { Alert, Platform } from "react-native";
import { pairingScopeCopy } from "./pairingScope";

export function confirmPairingScope(): Promise<boolean> {
  const { title, message } = pairingScopeCopy(Platform.OS === "web" ? "browser" : "phone");
  return new Promise((resolve) => {
    Alert.alert(title, message,
      [{ text: "Cancel", style: "cancel", onPress: () => resolve(false) },
        { text: "Pair and grant access", onPress: () => resolve(true) }],
      { cancelable: true, onDismiss: () => resolve(false) });
  });
}
