import AsyncStorage from "@react-native-async-storage/async-storage";
import * as Notifications from "expo-notifications";

import { shouldAskForPushPermission } from "./pushPermissionPolicy";

const askedKey = "mewla.push.permission_asked.v1";

/** Resolves true once notifications are allowed, asking at most once ever. */
export async function ensurePushPermission(): Promise<boolean> {
  const current = await Notifications.getPermissionsAsync();
  if (current.granted) return true;
  let alreadyAsked = false;
  try {
    alreadyAsked = (await AsyncStorage.getItem(askedKey)) === "1";
  } catch {
    // Unreadable storage must not turn into a prompt on every connection.
    alreadyAsked = true;
  }
  if (!shouldAskForPushPermission(current, alreadyAsked)) return false;
  try {
    await AsyncStorage.setItem(askedKey, "1");
  } catch {
    // Asked anyway; the system keeps its own denial count.
  }
  return (await Notifications.requestPermissionsAsync()).granted;
}
