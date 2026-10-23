import * as Storage from "./storage";

/**
 * Makes a paired computer the one in use. Settings, the menu's computer
 * switcher and a computer's message card all switch through here: the computer
 * is also allowed to connect, so one turned off on this phone comes up.
 */
export async function switchToComputer(
  serverId: string,
  switchCurrentServer: (serverId: string) => Promise<void>,
  allowConnect: (serverId: string, enabled: boolean) => Promise<unknown> = Storage.setServerAutoConnect,
): Promise<void> {
  await allowConnect(serverId, true);
  await switchCurrentServer(serverId);
}
