import { signal } from "nsh";
import { getServers, removeServer as forgetServer, serverNamedByAddress, updateServer, type StoredServer } from "../../../core/servers";
import { checkComputer, revokeThisDevice, type ComputerCheck } from "../../../core/settings/computer";
import { computerStatusWord } from "../../../core/settings/computerModel";
import { connection, current, problem, servers, switchTo } from "../../session";

/** What each computer answered when Settings last asked (the current one answers through its connection). */
export const checks = signal<Record<string, ComputerCheck>>({});

/**
 * Asks every computer whether it is up and still trusts this phone. A computer still
 * named by its address takes the name it reports.
 */
export async function probeComputers(): Promise<void> {
  const list = servers();
  const answers = await Promise.all(list.map(async (server) => [server, await checkComputer(server)] as const));
  const next: Record<string, ComputerCheck> = {};
  let renamed = false;
  for (const [server, check] of answers) {
    next[server.id] = check;
    if (check.state === "online" && check.name && serverNamedByAddress(server) && check.name !== server.name) {
      await updateServer(server.id, { name: check.name });
      renamed = true;
    }
  }
  checks.set(next);
  if (renamed) await reloadServers();
}

export async function reloadServers(): Promise<void> {
  const list = await getServers();
  servers.set(list);
  const now = current();
  const fresh = now ? list.find((s) => s.id === now.id) : null;
  if (fresh && (fresh.name !== now!.name || fresh.url !== now!.url)) current.set(fresh);
}

/** Why the current computer refuses this phone, from the connection's last problem. */
export function currentIssue(): "device_not_paired" | "other" | null {
  if (connection() === "connected" || !problem()) return null;
  return /not paired|unpaired|401|unauthori[sz]ed|pair again/i.test(problem()) ? "device_not_paired" : "other";
}

export function statusWord(server: StoredServer): string {
  const isCurrent = server.id === current()?.id;
  return computerStatusWord({
    current: isCurrent,
    connection: isCurrent ? connection() : undefined,
    issue: isCurrent ? currentIssue() : null,
    check: checks()[server.id],
  });
}

/**
 * Forgets a computer on this phone: asks it to forget this phone first (best effort),
 * then removes it; when it was the current one, the first other computer takes over.
 */
export async function removeComputer(server: StoredServer): Promise<void> {
  await revokeThisDevice(server);
  const wasCurrent = current()?.id === server.id;
  await forgetServer(server.id);
  const list = await getServers();
  servers.set(list);
  if (wasCurrent) await switchTo(list[0] ?? null);
}
