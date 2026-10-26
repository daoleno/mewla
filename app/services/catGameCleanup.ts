import AsyncStorage from "@react-native-async-storage/async-storage";
import { Directory, Paths } from "expo-file-system";
import { Platform } from "react-native";

const CAT_KEY_PREFIXES = [
  "mewla.cat.labs.v1:",
  "mewla.cat.view.v2:",
  "mewla.cat.bundle.last.v1:",
  "mewla.cat.bundle.order.v1",
];

/** The cat game is gone: drop what it left on this device, once per launch. */
export async function removeCatGameData(): Promise<void> {
  try {
    const keys = await AsyncStorage.getAllKeys();
    const stale = keys.filter((key) => CAT_KEY_PREFIXES.some((prefix) => key.startsWith(prefix)));
    if (stale.length > 0) await AsyncStorage.multiRemove(stale);
  } catch {
    // Storage unavailable: nothing reads these keys any more.
  }
  if (Platform.OS === "web") return;
  try {
    const bundle = new Directory(Paths.document, "cat");
    if (bundle.exists) bundle.delete();
  } catch {
    // Leftover files are inert; the next launch tries again.
  }
}
