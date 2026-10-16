import { checkComputer, joinComputers, type ComputerCheck } from "./computerTeam";
import { invalidateStoredServerTransport } from "./pinnedTransport";
import * as Storage from "./storage";
import { wsClient } from "./websocket";

/**
 * Asks every computer on this phone how it is, names the ones still known by
 * their address after what they call themselves, then lets the ones that
 * answered work together. `onChecked` hears the answers before the slower
 * introductions start.
 */
export async function syncComputers(
  onChecked?: (checks: Record<string, ComputerCheck>, renamed: boolean) => void | Promise<void>,
): Promise<void> {
  const saved = await Storage.getServers();
  const results = await Promise.all(saved.map(async (server) => [server, await checkComputer(server)] as const));
  let renamed = false;
  for (const [server, check] of results) {
    if (check.state === "online" && check.name && Storage.serverNamedByAddress(server)) {
      await Storage.updateServer(server.id, { name: check.name });
      renamed = true;
    }
  }
  await onChecked?.(Object.fromEntries(results.map(([server, check]) => [server.id, check])), renamed);
  const online = new Set(results.filter(([, check]) => check.state === "online").map(([server]) => server.id));
  const latest = renamed ? await Storage.getServers() : saved;
  try {
    await joinComputers(latest.filter((server) => online.has(server.id)));
  } finally {
    // Close the Link tunnels these checks opened, except the one in use.
    for (const server of saved) {
      if (wsClient.liveServerId() !== server.id) void invalidateStoredServerTransport(server);
    }
  }
}
