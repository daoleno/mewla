import { effect, platform, signal, untracked } from "nsh";
import { getServers, removeServer as forgetServer, serverNamedByAddress, updateServer, type StoredServer } from "../../../core/servers";
import { checkComputer, revokeThisDevice, type ComputerCheck } from "../../../core/settings/computer";
import { computerStatusWord } from "../../../core/settings/computerModel";
import { invalidateStoredServerTransport } from "../../../core/link/pinnedTransport";
import { computersApartFrom, forgetEverywhere, joinComputers, missedComputersMessage, onComputersJoined } from "../../../core/settings/computerTeam";
import { diagnoseConnectionIssue, type ConnectionIssue } from "../../../core/settings/connectionIssue";
import { pathMark, pathOfConnection, type PathMark } from "../../../core/settings/connectionPath";
import { clearCalendarAlerts } from "../../calendar/state";
import { connection, current, problem, servers, switchTo } from "../../session";

/** What each computer answered when Settings last asked (the current one answers through its connection). */
export const checks = signal<Record<string, ComputerCheck>>({});

let probing: Promise<void> | null = null;

/**
 * Asks every computer whether it is up and still trusts this phone. A computer still
 * named by its address takes the name it reports; then the ones that answered are
 * introduced to each other, so they can hand each other work.
 */
export function probeComputers(): Promise<void> {
  if (probing) return probing;
  probing = probeAll().finally(() => {
    probing = null;
  });
  return probing;
}

async function probeAll(): Promise<void> {
  const list = servers();
  const answers = await Promise.all(
    list.map(async (server) => {
      const check = await checkComputer(server);
      // A Mewla Relay computer that is not current keeps no tunnel open after its check.
      if (server.id !== current()?.id) await invalidateStoredServerTransport(server);
      return [server, check] as const;
    }),
  );
  const next: Record<string, ComputerCheck> = {};
  let renamed = false;
  for (const [server, check] of answers) {
    next[server.id] = check;
    if (await adoptReportedName(server, check)) renamed = true;
  }
  checks.set(next);
  if (renamed) await reloadServers();
  const online = servers().filter((server) => next[server.id]?.state === "online");
  void joinComputers(online).catch(() => {});
}

/** Asks one computer again (its Computer page), keeping what the others answered. */
export async function probeComputer(server: StoredServer): Promise<void> {
  const check = await checkComputer(server);
  checks.set({ ...checks(), [server.id]: check });
  if (await adoptReportedName(server, check)) await reloadServers();
}

/** A computer still named by its address takes the name it reports. */
async function adoptReportedName(server: StoredServer, check: ComputerCheck): Promise<boolean> {
  if (check.state !== "online" || !check.name || !serverNamedByAddress(server) || check.name === server.name) return false;
  await updateServer(server.id, { name: check.name });
  return true;
}

export async function reloadServers(): Promise<void> {
  const list = await getServers();
  servers.set(list);
  const now = current();
  const fresh = now ? list.find((s) => s.id === now.id) : null;
  if (fresh && (fresh.name !== now!.name || fresh.url !== now!.url)) current.set(fresh);
}

/** Bumped after each introduction round, so the "Can't reach" rows follow it. */
const teamRound = signal(0);
onComputersJoined(() => teamRound.set(teamRound() + 1));

/** The names of the computers `server` could not be introduced to. */
export function apartNames(server: StoredServer): string[] {
  teamRound();
  const others = servers().filter((other) => other.id !== server.id);
  return computersApartFrom(server)
    .map((daemonId) => others.find((other) => other.daemonId === daemonId)?.name)
    .filter((name): name is string => Boolean(name));
}

/** Why the current computer's connection fails, once the phone has looked (cleared when it connects or another is current). */
export const connectionIssue = signal<{ serverId: string; issue: ConnectionIssue } | null>(null);
let diagnosed = "";
let followingIssue = false;

/**
 * Looks for the reason each time the current computer's connection fails with a new one.
 * Started once, at module load, so no page owns (and disposes) it.
 */
function followConnectionIssue(): void {
  if (followingIssue) return;
  followingIssue = true;
  effect(() => {
    const server = current();
    const state = connection();
    const why = problem();
    untracked(() => {
      if (!server || state === "connected") {
        diagnosed = "";
        if (connectionIssue()) connectionIssue.set(null);
        return;
      }
      if (connectionIssue() && connectionIssue()!.serverId !== server.id) connectionIssue.set(null);
      if (state !== "waiting" || !why) return;
      const key = `${server.id}|${server.url}|${why}`;
      if (key === diagnosed) return;
      diagnosed = key;
      void diagnoseConnectionIssue({ server, socketError: why }).then((issue) => {
        if (current()?.id === server.id && connection() !== "connected") connectionIssue.set({ serverId: server.id, issue });
      });
    });
  });
}

followConnectionIssue();

/** Look again on the next failure (Try again). */
export function forgetConnectionIssue(): void {
  diagnosed = "";
  connectionIssue.set(null);
}

/** The current computer's issue, when the phone has found one. */
export function currentConnectionIssue(): ConnectionIssue | null {
  const found = connectionIssue();
  return found && found.serverId === current()?.id && connection() !== "connected" ? found.issue : null;
}

/** Why the current computer refuses this phone: what the phone found when it looked, or that it fails at all. */
export function currentIssue(): "device_not_paired" | "other" | null {
  const now = current();
  if (!now || connection() === "connected") return null;
  const found = currentConnectionIssue();
  if (found) return found.code === "device_not_paired" ? "device_not_paired" : "other";
  if (checks()[now.id]?.state === "unpaired") return "device_not_paired";
  return problem() && connection() === "waiting" ? "other" : null;
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
 * How this phone reaches the current computer, while it is connected. A browser reaches
 * only the computer serving the page, through the page's origin: no mark there (as v0.3.1).
 */
export function currentPathMark(): PathMark | null {
  const now = current();
  return now && platform.os !== "web" ? pathMark(pathOfConnection(now), connection() === "connected") : null;
}

/** What the last removal said about the computers that did not answer; Settings shows it until dismissed. */
export const removedNotice = signal<{ title: string; detail: string } | null>(null);

/**
 * Removes a computer: every other computer forgets it, it forgets this phone (best
 * effort, bounded), then the phone forgets it. When it was the current one its calendar
 * alerts stop and the first other computer takes over. Returns what to say about the
 * computers that did not answer, if any.
 */
export async function removeComputer(server: StoredServer): Promise<{ title: string; detail: string } | null> {
  const others = servers().filter((other) => other.id !== server.id);
  const missed = await forgetEverywhere(server, others);
  await revokeThisDevice(server);
  const wasCurrent = current()?.id === server.id;
  await forgetServer(server.id);
  const list = await getServers();
  servers.set(list);
  const { [server.id]: _gone, ...rest } = checks();
  checks.set(rest);
  if (wasCurrent) {
    clearCalendarAlerts();
    await switchTo(list[0] ?? null);
  }
  return missedComputersMessage(server, missed);
}
