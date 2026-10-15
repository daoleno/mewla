import { Alert, Platform } from "react-native";
import { parseConnectLink } from "./connection";
import { alreadyPairedWith, pairingScopeCopy } from "./pairingScope";
import { getServers } from "./storage";

export function confirmPairingScope(): Promise<boolean> {
  const { title, message } = pairingScopeCopy(Platform.OS === "web" ? "browser" : "phone");
  return new Promise((resolve) => {
    Alert.alert(title, message,
      [{ text: "Cancel", style: "cancel", onPress: () => resolve(false) },
        { text: "Pair and grant access", onPress: () => resolve(true) }],
      { cancelable: true, onDismiss: () => resolve(false) });
  });
}

/** Ask only when the link's server is not one this device already paired. */
export async function confirmPairingScopeFor(rawLink: string): Promise<boolean> {
  if (alreadyPairedWith(await getServers(), parseConnectLink(rawLink))) return true;
  return confirmPairingScope();
}
