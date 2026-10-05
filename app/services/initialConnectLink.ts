import * as Linking from "expo-linking";

/** The URL that launched the app, which may carry a pairing link. */
export function readInitialConnectLink(): Promise<string | null> {
  return Linking.getInitialURL();
}
