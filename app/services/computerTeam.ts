import { getOrCreateLocalDeviceIdentity, type LocalDeviceIdentity } from "./auth";
import { verifyDaemonAssertion } from "./deviceAuthContract";
import { otherComputerURLs } from "./computerAddresses";
import { connectComputers, forgetComputer, listComputers, type ComputerPeer } from "./computers";
import { peersForDaemon } from "./computerPeers";
import { signedFetch } from "./signedRequest";
import type { StoredServer } from "./storage";

/** What this phone found when it last asked a computer. */
export type ComputerCheck =
  | { state: "online"; name?: string }
  | { state: "unpaired" }
  | { state: "offline" };

const CHECK_TIMEOUT_MS = 8_000;

// checkComputer asks one computer, current or not, whether it is up and still
// trusts this phone, and what it is called.
export async function checkComputer(
  server: StoredServer,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<ComputerCheck> {
  try {
    const identity = await loadIdentity();
    const response = await withTimeout(
      signedFetch(server, "GET", "/auth-check", "mewla-probe", undefined, identity),
      CHECK_TIMEOUT_MS,
    );
    if (response.status === 401) return { state: "unpaired" };
    if (!response.ok) return { state: "offline" };
    const body = (await response.json()) as {
      name?: unknown;
      assertion_timestamp?: string;
      assertion_nonce?: string;
      assertion_signature?: string;
    };
    const genuine = verifyDaemonAssertion({
      purpose: "mewla-probe",
      daemonId: server.daemonId,
      daemonPublicKey: server.daemonPublicKey,
      timestamp: body.assertion_timestamp,
      nonceHex: body.assertion_nonce,
      signatureHex: body.assertion_signature,
    });
    // Something else answering at the address is not this computer.
    if (!genuine) return { state: "offline" };
    const name = typeof body.name === "string" ? body.name.trim() : "";
    return name ? { state: "online", name } : { state: "online" };
  } catch {
    return { state: "offline" };
  }
}

/** Two computers on this phone that could not reach each other. */
export interface ComputersApart {
  daemonIds: [string, string];
  at: number;
}

export interface TeamDeps {
  list: (server: StoredServer) => Promise<ComputerPeer[]>;
  connect: (from: StoredServer, to: StoredServer) => Promise<void>;
  now: () => number;
}

const defaultDeps: TeamDeps = {
  list: (server) => listComputers(server),
  connect: (from, to) => connectComputers(from, to),
  now: () => Date.now(),
};

// A pair that failed is not tried again on every visit to Settings.
const RETRY_APART_MS = 10 * 60_000;
const apart = new Map<string, ComputersApart>();
let running: Promise<void> | null = null;

function pairKey(left: string, right: string): string {
  return [left, right].sort().join(":");
}

/**
 * Lets every two of `servers` (computers paired with this phone that answer
 * now) hand each other work. This phone, trusted by both, introduces them;
 * one connection in either direction carries messages both ways, so a pair
 * already connected either way is left alone.
 */
export function joinComputers(servers: readonly StoredServer[], deps: TeamDeps = defaultDeps): Promise<void> {
  if (running) return running;
  running = joinAll(servers, deps).finally(() => {
    running = null;
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
        // Unreachable now; its pairs wait for a later visit.
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
      if (
        peersForDaemon(leftPeers, right.daemonId).length > 0 ||
        peersForDaemon(rightPeers, left.daemonId).length > 0
      ) {
        apart.delete(key);
        continue;
      }
      const failed = apart.get(key);
      if (failed && deps.now() - failed.at < RETRY_APART_MS) continue;
      // Dial the one with an address another computer can reach first; a
      // computer reached only through Mewla Link can still dial out.
      const [from, to] =
        otherComputerURLs(right).length === 0 && otherComputerURLs(left).length > 0
          ? [right, left]
          : [left, right];
      try {
        await deps.connect(from, to);
        apart.delete(key);
        continue;
      } catch {
        // Try the other way.
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

/** The daemon ids of computers that `server` could not be connected with. */
export function computersApartFrom(server: StoredServer): string[] {
  const ids: string[] = [];
  for (const entry of apart.values()) {
    const [left, right] = entry.daemonIds;
    if (left === server.daemonId) ids.push(right);
    else if (right === server.daemonId) ids.push(left);
  }
  return ids;
}

/**
 * Before `removed` leaves this phone, every other computer forgets it and it
 * forgets them. Returns the computers that could not be told; they keep
 * working with `removed` until they are told there.
 */
export async function forgetEverywhere(
  removed: StoredServer,
  others: readonly StoredServer[],
  forget: (current: StoredServer, otherDaemonId: string, other: StoredServer) => Promise<unknown> = forgetComputer,
): Promise<StoredServer[]> {
  const missed: StoredServer[] = [];
  await Promise.all(
    others
      .filter((other) => other.id !== removed.id && other.daemonId !== removed.daemonId)
      .map(async (other) => {
        try {
          await withTimeout(forget(other, removed.daemonId, removed), CHECK_TIMEOUT_MS);
        } catch {
          missed.push(other);
        }
      }),
  );
  return missed;
}

export function resetComputerTeamForTests() {
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
