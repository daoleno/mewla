import { useSyncExternalStore } from "react";
import { Platform } from "react-native";
import type { TransportPathInfo } from "./connectionPath";
import { subscribeTransportPath, transportPathInfo } from "./pinnedTransport";

/**
 * The live path of the app's connection to a computer: LAN, Tailscale or the
 * relay. The browser always goes through the relay's HTTPS, where direct is
 * not possible, so it has no path to show.
 */
export function useTransportPath(serverId: string | null | undefined): TransportPathInfo | null {
  return useSyncExternalStore(
    subscribeTransportPath,
    () => (serverId && Platform.OS !== "web" ? transportPathInfo(serverId) : null),
    () => null,
  );
}
