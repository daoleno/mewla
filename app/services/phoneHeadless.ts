import Constants from "expo-constants";
import { AppRegistry, Platform } from "react-native";
import { MewlaPhone, parsePhoneStatus } from "../modules/mewla-phone/src";
import { resolveCurrentServerId, selectCurrentServer } from "./currentServerSelection";
import { phoneLinkHost } from "./phoneLinkProcess";
import { getCurrentServerId, getServers } from "./storage";

/**
 * The native side starts this with no screen (after a reboot, or once the App was swiped away) so
 * the computer can reach the phone again. It brings up the same link the Phone page would.
 */
export async function runPhoneLinkTask(): Promise<void> {
  if (!MewlaPhone || !parsePhoneStatus(MewlaPhone.status())?.enabled) return;
  const [servers, persistedId] = await Promise.all([getServers(), getCurrentServerId()]);
  const server = selectCurrentServer(servers, resolveCurrentServerId(servers, persistedId));
  if (!server) return;
  phoneLinkHost.hold({ server, native: MewlaPhone, appVersion: String(Constants.expoConfig?.version ?? "") });
}

if (Platform.OS === "android" && MewlaPhone) {
  AppRegistry.registerHeadlessTask("MewlaPhoneLink", () => runPhoneLinkTask);
  // The foreground notification says whether the computer can reach the phone, with or without a screen.
  phoneLinkHost.watch((state) => MewlaPhone?.setLink?.(state === "online"));
}
