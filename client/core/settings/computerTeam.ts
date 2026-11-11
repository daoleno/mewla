import { verifyDaemonAssertion } from "../deviceAuthContract";
import { answerError, bodyPurpose, signedFetch } from "../more/daemon";
import type { StoredServer } from "../storedServerContract";

/**
 * The computers paired with this phone, working as one team: the phone, trusted by
 * every one of them, introduces each two that answer so they can hand each other
 * work, and when one is removed, tells the others to forget it.
 */

/** A computer another one works with, as that one lists it (GET /computers). */
export interface ComputerPeer {
  name: string;
  daemon_id?: string;
  device_id?: string;
  direction: "outbound" | "inbound";
  online: boolean;
  last_seen?: string;
  version?: string;
  last_error?: string;
}

/** A computer's entries in a peer list, either way: an inbound one may know only the start of the daemon id, from its device id. */
export function peersForDaemon(peers: readonly ComputerPeer[], daemonId: string): ComputerPeer[] {
  const id = daemonId.toLowerCase();
  if (!id) return [];
  return peers.filter((peer) => {
    if (peer.daemon_id) return peer.daemon_id.toLowerCase() === id;
    const prefix = peer.device_id?.replace(/^computer-/, "").toLowerCase();
    return Boolean(prefix && prefix.length >= 16 && id.startsWith(prefix));
  });
}

/** The addresses of `other` another computer could dial. The phone's own Link tunnel is never passed on. */
export function otherComputerURLs(other: StoredServer): string[] {
  const urls: string[] = [];
  const add = (value: string | undefined) => {
    const trimmed = value?.trim();
    if (!trimmed) return;
    try {
      const parsed = new URL(trimmed);
      if (parsed.protocol === "ws:") parsed.protocol = "http:";
      if (parsed.protocol === "wss:") parsed.protocol = "https:";
      if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return;
      const base = `${parsed.protocol}//${parsed.host}`;
      if (!urls.includes(base)) urls.push(base);
    } catch {
      // Not an address.
    }
  };
  if (other.transportKind !== "link") add(other.url);
  for (const candidate of other.transportCandidates ?? []) {
    if (candidate.kind === "manual") add(candidate.url);
  }
  return urls;
}

/** The computers `server` works with. */
export async function listComputers(server: StoredServer): Promise<ComputerPeer[]> {
  const response = await signedFetch(server, "GET", "/computers", "mewla-computer-admin:list:GET:/computers");
  if (!response.ok) throw await answerError(server, response);
  const body = (await response.json()) as { computers?: ComputerPeer[] };
  return body.computers ?? [];
}

/**
 * Lets `from` hand work to `to`: the phone asks `to` for a one-time invite and gives it
 * to `from`, which pairs with `to` itself and pins the key this phone pinned.
 */
export async function connectComputers(from: StoredServer, to: StoredServer): Promise<void> {
  const inviteResponse = await signedFetch(to, "POST", "/computers/invite", "mewla-computer-admin:invite:POST:/computers/invite");
  if (!inviteResponse.ok) throw await answerError(to, inviteResponse);
  const invite = (await inviteResponse.json()) as { invite?: string; assertion_timestamp?: string; assertion_nonce?: string; assertion_signature?: string };
  const signed = verifyDaemonAssertion({
    purpose: "mewla-computer-invite",
    daemonId: to.daemonId,
    daemonPublicKey: to.daemonPublicKey,
    timestamp: invite.assertion_timestamp,
    nonceHex: invite.assertion_nonce,
    signatureHex: invite.assertion_signature,
  });
  if (!signed || !invite.invite) throw new Error(`${to.name} did not answer with its own key`);
  const response = await signedFetch(from, "POST", "/computers", bodyPurpose("mewla-computer-admin:add:POST:/computers"), {
    body: { invite: invite.invite, name: to.name, urls: otherComputerURLs(to), expected_daemon_id: to.daemonId },
  });
  if (!response.ok) throw await answerError(from, response);
}

async function forgetOn(server: StoredServer, daemonId: string): Promise<void> {
  const response = await signedFetch(server, "DELETE", "/computers", bodyPurpose("mewla-computer-admin:remove:DELETE:/computers"), {
    body: { expected_daemon_id: daemonId },
  });
  if (!response.ok) throw await answerError(server, response);
}

/**
 * Ends working together between `current` and the computer whose daemon is `otherDaemonId`,
 * both ways: `current` forgets it, and `other` forgets `current` when this phone knows it.
 * Throws when `current` could not be told.
 */
export async function forgetComputer(current: StoredServer, otherDaemonId: string, other: StoredServer | null): Promise<void> {
  await forgetOn(current, otherDaemonId);
  if (other) await forgetOn(other, current.daemonId).catch(() => {});
}

/** Two computers on this phone that could not be introduced. */
interface ComputersApart {
  daemonIds: [string, string];
  at: number;
}

export interface TeamDeps {
  list: (server: StoredServer) => Promise<ComputerPeer[]>;
  connect: (from: StoredServer, to: StoredServer) => Promise<void>;
  now: () => number;
}

const defaultDeps: TeamDeps = { list: listComputers, connect: connectComputers, now: () => Date.now() };

/** A pair that could not meet is not tried again on every visit to Settings. */
export const RETRY_APART_MS = 10 * 60_000;
const apart = new Map<string, ComputersApart>();
let running: Promise<void> | null = null;
const listeners = new Set<() => void>();

const pairKey = (left: string, right: string) => [left, right].sort().join(":");

/**
 * Introduces every two of `servers` (computers that answer now) to each other. One
 * connection in either direction carries work both ways, so a pair already connected
 * either way is left alone. One run at a time; a second call joins the first.
 */
export function joinComputers(servers: readonly StoredServer[], deps: TeamDeps = defaultDeps): Promise<void> {
  if (running) return running;
  running = joinAll(servers, deps).finally(() => {
    running = null;
    listeners.forEach((listener) => listener());
  });
  return running;
}

async function joinAll(servers: readonly StoredServer[], deps: TeamDeps): Promise<void> {
  if (servers.length < 2) return;
  const peers = new Map<string, ComputerPeer[]>();
  await Promise.all(
    servers.map(async (server) => {
      try {
        peers.set(server.id, await deps.list(server));
      } catch {
        // It does not answer now; its pairs wait for a later visit.
      }
    }),
  );
  for (let i = 0; i < servers.length; i += 1) {
    for (let j = i + 1; j < servers.length; j += 1) {
      const left = servers[i];
      const right = servers[j];
      const leftPeers = peers.get(left.id);
      const rightPeers = peers.get(right.id);
      if (!leftPeers || !rightPeers || left.daemonId === right.daemonId) continue;
      const key = pairKey(left.daemonId, right.daemonId);
      if (peersForDaemon(leftPeers, right.daemonId).length > 0 || peersForDaemon(rightPeers, left.daemonId).length > 0) {
        apart.delete(key);
        continue;
      }
      const failed = apart.get(key);
      if (failed && deps.now() - failed.at < RETRY_APART_MS) continue;
      // The one with an address another computer can reach is dialled; a computer
      // reached only through Mewla Link can still dial out.
      const [from, to] = otherComputerURLs(right).length === 0 && otherComputerURLs(left).length > 0 ? [right, left] : [left, right];
      try {
        await deps.connect(from, to);
        apart.delete(key);
        continue;
      } catch {
        // The other way round, next.
      }
      try {
        await deps.connect(to, from);
        apart.delete(key);
      } catch {
        apart.set(key, { daemonIds: [left.daemonId, right.daemonId], at: deps.now() });
      }
    }
  }
}

/** Whether computers are being introduced now. */
export function joiningComputers(): boolean {
  return running !== null;
}

/** Runs `listener` after every introduction round. */
export function onComputersJoined(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** The daemon ids of the computers `server` could not be introduced to. */
export function computersApartFrom(server: StoredServer): string[] {
  const ids: string[] = [];
  for (const entry of apart.values()) {
    const [left, right] = entry.daemonIds;
    if (left === server.daemonId) ids.push(right);
    else if (right === server.daemonId) ids.push(left);
  }
  return ids;
}

/** How long each other computer gets to answer when one is removed. */
export const FORGET_TIMEOUT_MS = 8_000;

/**
 * Before `removed` leaves this phone, every other computer forgets it (and it them).
 * Returns the ones that could not be told: they keep working with `removed` until told there.
 */
export async function forgetEverywhere(
  removed: StoredServer,
  others: readonly StoredServer[],
  forget: (current: StoredServer, otherDaemonId: string, other: StoredServer) => Promise<unknown> = forgetComputer,
  timeoutMs = FORGET_TIMEOUT_MS,
): Promise<StoredServer[]> {
  const missed: StoredServer[] = [];
  await Promise.all(
    others
      .filter((other) => other.id !== removed.id && other.daemonId !== removed.daemonId)
      .map(async (other) => {
        try {
          await withTimeout(forget(other, removed.daemonId, removed), timeoutMs);
        } catch {
          missed.push(other);
        }
      }),
  );
  return others.filter((other) => missed.includes(other));
}

/** What a removal says when some computers did not answer. */
export function missedComputersMessage(removed: StoredServer, missed: readonly StoredServer[]): { title: string; detail: string } | null {
  if (missed.length === 0) return null;
  return {
    title: `${missed.map((other) => other.name).join(", ")} didn't answer`,
    detail: `It still works with ${removed.name}. On it, run mewla computers remove "${removed.name}".`,
  };
}

export function resetComputerTeamForTests(): void {
  apart.clear();
  running = null;
}

function withTimeout<T>(promise: Promise<T>, ms: number): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("timed out")), ms);
    promise.then(
      (value) => {
        clearTimeout(timer);
        resolve(value);
      },
      (error) => {
        clearTimeout(timer);
        reject(error);
      },
    );
  });
}
