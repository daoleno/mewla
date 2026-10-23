import {
  directAddressesFor,
  verifyHealthChallenge,
  type DirectAddress,
  type DirectKind,
} from "./directAddresses";
import type { StoredServer, StoredTransportCandidate } from "./storage";

/** How the phone reaches a computer: through Mewla Link's relay, or straight. */
export type TransportPath = "relay" | DirectKind;

interface Selection {
  path: TransportPath;
  remoteURL: string;
  localOrigin: string;
  tunnelKey: string;
  rttMs: number;
  measuredAt: number;
}

// Per computer. `relay` stays open as the fallback while `current` is direct,
// so a lost direct address moves to the relay without measuring it again.
interface Paths {
  relay?: Selection;
  direct?: Selection;
  current?: "relay" | "direct";
}

const paths = new Map<string, Paths>();
const directSuppressedUntil = new Map<string, number>();
const resolving = new Map<string, Promise<string>>();
const directProbes = new Map<string, Promise<Selection | null>>();
const SELECTION_TTL_MS = 30_000;
// A direct address that cannot be verified in this long is treated as
// unreachable; the relay is already being measured meanwhile.
const DIRECT_PROBE_CAP_MS = 1_200;
// When the relay answers first, a direct answer this close behind still wins.
const DIRECT_GRACE_MS = 250;
const DIRECT_SUPPRESS_MS = 20_000;

function pathsFor(serverId: string): Paths {
  let entry = paths.get(serverId);
  if (!entry) {
    entry = {};
    paths.set(serverId, entry);
  }
  return entry;
}

function fresh(selection: Selection): boolean {
  return Date.now() - selection.measuredAt < SELECTION_TTL_MS;
}

/** The path the phone is using for a computer, or null before it has chosen. */
export function currentTransportPath(serverId: string): TransportPath | null {
  const entry = paths.get(serverId);
  if (!entry?.current) return null;
  return entry.current === "relay" ? "relay" : (entry.direct?.path ?? null);
}

export async function resolveStoredServerURL(
  server: StoredServer,
): Promise<string> {
  if (server.transportKind !== "link") {
    return server.url;
  }
  if (
    !/^[0-9a-f]{64}$/i.test(server.transportPin || "") ||
    !/^[0-9a-f]{32}$/i.test(server.linkRouteId || "")
  ) {
    throw new Error(
      "Mewla Link needs setup. Scan a fresh Mewla Link QR code; the saved route or transport pin is missing.",
    );
  }
  const entry = pathsFor(server.id);
  const direct = stillListed(server, entry.direct);
  if (direct && fresh(direct)) {
    entry.current = "direct";
    return rewriteSelection(server, direct);
  }
  const relay = entry.relay;
  if (
    entry.current === "relay" &&
    relay &&
    fresh(relay) &&
    linkCandidates(server).some((candidate) => candidate.url === relay.remoteURL)
  ) {
    return rewriteSelection(server, relay);
  }

  // Callers that arrive while a choice is being made share it. Tunnels are
  // keyed per address, so two overlapping races could stop each other's.
  let flight = resolving.get(server.id);
  if (!flight) {
    flight = chooseTransport(server, entry).finally(() =>
      resolving.delete(server.id),
    );
    resolving.set(server.id, flight);
  }
  return flight;
}

async function chooseTransport(
  server: StoredServer,
  entry: Paths,
): Promise<string> {
  // A direct path that worked a moment ago is re-verified for its full budget
  // before the relay is considered; only a path never seen working gets the
  // short grace below.
  const expectDirect = !!stillListed(server, entry.direct);
  const relayAttempt = measureRelay(server, entry);
  const directAttempt = probeDirect(server, entry);
  relayAttempt.catch(() => undefined);

  const first = expectDirect
    ? { direct: await directAttempt }
    : await Promise.race([
        directAttempt.then((selected) => ({ direct: selected })),
        relayAttempt.then(
          (selected) => ({ relay: selected }),
          () => ({ relay: null }),
        ),
      ]);
  let chosen: Selection | null;
  if ("direct" in first) {
    chosen = first.direct ?? (await relayAttempt.catch(() => null));
  } else if (first.relay) {
    const late = await Promise.race([directAttempt, sleep(DIRECT_GRACE_MS)]);
    chosen = late ?? first.relay;
  } else {
    chosen = await directAttempt;
  }
  if (!chosen) {
    throw new Error(
      "Mewla Link is offline. Keep mewla running and check its Link connection.",
    );
  }
  chosen.measuredAt = Date.now();
  entry.current = chosen.path === "relay" ? "relay" : "direct";
  return rewriteSelection(server, chosen);
}

// Dials every Link candidate, keeps the nearest one open as the relay path and
// closes the rest.
async function measureRelay(
  server: StoredServer,
  entry: Paths,
): Promise<Selection> {
  const candidates = linkCandidates(server);
  const cacheKey = `server:${server.id}`;
  const attempts = await Promise.all(
    candidates.map(async (candidate, index) => {
      const tunnelKey = `${cacheKey}:${index}`;
      try {
        const parsed = requireTLSServerURL(candidate.url);
        const result = await startNativePinnedTunnel(
          tunnelKey,
          parsed.hostname,
          parsed.port ? Number(parsed.port) : 443,
          server.transportPin!,
          "measure",
        );
        return {
          candidate,
          tunnelKey,
          localOrigin: `ws://127.0.0.1:${result.port}`,
          rttMs: result.rttMs,
        };
      } catch (error) {
        return { candidate, tunnelKey, error };
      }
    }),
  );
  const available = attempts
    .filter(
      (
        attempt,
      ): attempt is {
        candidate: StoredTransportCandidate;
        tunnelKey: string;
        localOrigin: string;
        rttMs: number;
      } => "localOrigin" in attempt,
    )
    .sort(
      (left, right) =>
        left.rttMs - right.rttMs ||
        left.candidate.name.localeCompare(right.candidate.name),
    );
  if (available.length === 0) {
    throw new Error(
      "Mewla Link is offline. Keep mewla running and check its Link connection.",
    );
  }
  const best = available[0];
  const selection: Selection = {
    path: "relay",
    remoteURL: best.candidate.url,
    localOrigin: best.localOrigin,
    tunnelKey: best.tunnelKey,
    rttMs: best.rttMs,
    measuredAt: Date.now(),
  };
  entry.relay = selection;
  await Promise.all(
    available
      .slice(1)
      .map((attempt) =>
        stopNativePinnedTunnel(attempt.tunnelKey).catch(() => undefined),
      ),
  );
  return selection;
}

// Tries the addresses the computer published and keeps the first that proves
// it is the paired daemon. Resolves null, never rejects, within
// DIRECT_PROBE_CAP_MS: an unreachable address must not delay the relay.
function probeDirect(
  server: StoredServer,
  entry: Paths,
): Promise<Selection | null> {
  let probe = directProbes.get(server.id);
  if (!probe) {
    probe = verifyDirect(server, entry).finally(() =>
      directProbes.delete(server.id),
    );
    directProbes.set(server.id, probe);
  }
  return probe;
}

async function verifyDirect(
  server: StoredServer,
  entry: Paths,
): Promise<Selection | null> {
  const addresses = directAddressesFor(server);
  if (addresses.length === 0) return null;
  if ((directSuppressedUntil.get(server.id) ?? 0) > Date.now()) return null;
  const current = stillListed(server, entry.direct);
  if (current && fresh(current)) return current;

  let winner: Selection | null = null;
  let rest = addresses;
  if (current) {
    // The path in use is checked on its own first: moving to another address
    // would close the tunnel the live connection is carried on.
    const inUse = addresses.find((address) => directURL(address) === current.remoteURL);
    rest = addresses.filter((address) => address !== inUse);
    winner = inUse ? await probeAddress(server, inUse, current) : null;
    if (!winner) delete entry.direct;
  }
  if (!winner) {
    winner = await firstVerified(
      rest.map((address) => probeAddress(server, address)),
      (late) => {
        if (late.tunnelKey !== entry.direct?.tunnelKey) {
          void stopNativePinnedTunnel(late.tunnelKey).catch(() => undefined);
        }
      },
    );
  }
  if (!winner) {
    directSuppressedUntil.set(server.id, Date.now() + DIRECT_SUPPRESS_MS);
    return null;
  }
  directSuppressedUntil.delete(server.id);
  entry.direct = winner;
  return winner;
}

function directURL(address: DirectAddress): string {
  return `wss://${address.host}:${address.port}`;
}

// `inUse` is the verified path a live connection is carried on. A re-check of
// it that is merely slow keeps it; only a connect error, a bad answer or a
// missing one drops it, because closing it would end that connection.
async function probeAddress(
  server: StoredServer,
  address: DirectAddress,
  inUse?: Selection,
): Promise<Selection | null> {
  const tunnelKey = `server:${server.id}:direct:${address.host}:${address.port}`;
  const abort = new AbortController();
  let timedOut = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const cap = new Promise<Selection | null>((resolve) => {
    timer = setTimeout(() => {
      timedOut = true;
      abort.abort();
      resolve(inUse ? { ...inUse, measuredAt: Date.now() } : null);
    }, DIRECT_PROBE_CAP_MS);
  });
  const keepOpen = () => timedOut && !!inUse;
  const attempt = (async (): Promise<Selection | null> => {
    // The TLS stream is pinned to the computer's transport key before a byte
    // of HTTP flows, and the challenge below ties it to the daemon key. Only
    // then is the address offered to the callers that send the device header.
    const tunnel = await startNativePinnedTunnel(
      tunnelKey,
      address.host,
      address.port,
      server.transportPin!,
      "measure",
    );
    if (timedOut || !(await answersChallenge(server, tunnel.port, abort.signal))) {
      if (!keepOpen()) await stopNativePinnedTunnel(tunnelKey).catch(() => undefined);
      return null;
    }
    return {
      path: address.kind,
      remoteURL: directURL(address),
      localOrigin: `ws://127.0.0.1:${tunnel.port}`,
      tunnelKey,
      rttMs: tunnel.rttMs,
      measuredAt: Date.now(),
    };
  })().catch(async () => {
    if (!keepOpen()) await stopNativePinnedTunnel(tunnelKey).catch(() => undefined);
    return null;
  });
  try {
    const selected = await Promise.race([attempt, cap]);
    if (!selected) {
      // Whatever the dial is still doing is closed when it lands.
      await stopNativePinnedTunnel(tunnelKey).catch(() => undefined);
    }
    return selected;
  } finally {
    clearTimeout(timer);
  }
}

async function answersChallenge(
  server: StoredServer,
  localPort: number,
  signal: AbortSignal,
): Promise<boolean> {
  const challengeHex = await newChallenge();
  const response = await fetch(
    `http://127.0.0.1:${localPort}/health?challenge=${challengeHex}`,
    { method: "GET", headers: { Accept: "application/json" }, signal },
  );
  if (!response.ok) return false;
  return verifyHealthChallenge({
    challengeHex,
    daemonId: server.daemonId,
    daemonPublicKey: server.daemonPublicKey,
    payload: await response.json(),
  });
}

async function newChallenge(): Promise<string> {
  const Crypto = await import("expo-crypto");
  return Array.from(Crypto.getRandomBytes(16), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

// The first non-null result, or null once all are null. A result that lands
// after the winner is handed to `discard`.
function firstVerified<T>(
  attempts: Promise<T | null>[],
  discard: (late: T) => void,
): Promise<T | null> {
  return new Promise((resolve) => {
    let pending = attempts.length;
    let won = false;
    if (pending === 0) resolve(null);
    for (const attempt of attempts) {
      attempt.then(
        (result) => {
          if (result && !won) {
            won = true;
            resolve(result);
          } else if (result) {
            discard(result);
          }
          if (--pending === 0 && !won) resolve(null);
        },
        () => {
          if (--pending === 0 && !won) resolve(null);
        },
      );
    }
  });
}

// A verified direct path is only trusted while the computer still lists it.
function stillListed(
  server: StoredServer,
  selection: Selection | undefined,
): Selection | undefined {
  if (!selection || selection.path === "relay") return undefined;
  return directAddressesFor(server).some(
    (address) => selection.remoteURL === directURL(address),
  )
    ? selection
    : undefined;
}

/**
 * Looks for a direct path while the phone is on the relay. True when one was
 * just verified, so the caller can reconnect onto it.
 */
export async function directPathBecameAvailable(
  server: StoredServer,
): Promise<boolean> {
  if (server.transportKind !== "link") return false;
  const entry = paths.get(server.id);
  if (entry?.current !== "relay") return false;
  return (await probeDirect(server, entry)) !== null;
}

/**
 * Asks the next resolve to verify a direct path afresh instead of trusting its
 * last check, for a connection that ended while it was in use.
 */
export function recheckDirectPath(serverId: string): void {
  const entry = paths.get(serverId);
  if (entry?.direct) entry.direct.measuredAt = 0;
}

/**
 * The live connection failed. A failed direct path is closed and the relay
 * kept ready; a failed relay is closed so it is measured afresh.
 */
export async function transportFailed(server: StoredServer): Promise<void> {
  const entry = paths.get(server.id);
  if (!entry?.current) {
    return invalidateStoredServerTransport(server);
  }
  const failed = entry.current === "direct" ? entry.direct : entry.relay;
  if (entry.current === "direct") delete entry.direct;
  else delete entry.relay;
  delete entry.current;
  if (failed) await stopNativePinnedTunnel(failed.tunnelKey).catch(() => undefined);
}

export async function invalidateStoredServerTransport(
  server: StoredServer,
): Promise<void> {
  if (server.transportKind !== "link") {
    return;
  }
  const entry = paths.get(server.id);
  paths.delete(server.id);
  await Promise.all(
    [entry?.relay, entry?.direct].map((selection) =>
      selection
        ? stopNativePinnedTunnel(selection.tunnelKey).catch(() => undefined)
        : undefined,
    ),
  );
}

export async function resolveCanonicalServerURL(
  server: StoredServer,
): Promise<string> {
  return resolveStoredServerURL(server);
}

export async function resolvePairingLinkURL(input: {
  routeId: string;
  transportPin: string;
  url: string;
}): Promise<string> {
  return resolveOnDemandLinkURL(`pair:${input.routeId}`, input);
}

export async function releasePairingLinkTransport(
  routeId: string,
): Promise<void> {
  await stopNativePinnedTunnel(`pair:${routeId}`).catch(() => undefined);
}

// The confirmation tunnel reaches the route's stable URL, never the one-time
// admission, so it can ask an already-paired daemon about this phone.
export async function resolvePairingConfirmURL(input: {
  routeId: string;
  transportPin: string;
  url: string;
}): Promise<string> {
  return resolveOnDemandLinkURL(`pair-confirm:${input.routeId}`, input);
}

export async function releasePairingConfirmTransport(
  routeId: string,
): Promise<void> {
  await stopNativePinnedTunnel(`pair-confirm:${routeId}`).catch(
    () => undefined,
  );
}

async function resolveOnDemandLinkURL(
  key: string,
  input: { transportPin: string; url: string },
): Promise<string> {
  const parsed = requireTLSServerURL(input.url);
  let result: Awaited<ReturnType<typeof startNativePinnedTunnel>>;
  try {
    result = await startNativePinnedTunnel(
      key,
      parsed.hostname,
      parsed.port ? Number(parsed.port) : 443,
      input.transportPin,
      "on-demand",
    );
  } catch {
    throw new Error(
      "Mewla Link could not reach this computer. Keep mewla running, wait for Link to show connected, then scan a fresh QR.",
    );
  }
  return rewriteURL(input.url, `ws://127.0.0.1:${result.port}`);
}

async function startNativePinnedTunnel(
  key: string,
  host: string,
  port: number,
  pin: string,
  mode: "measure" | "on-demand",
) {
  const { startPinnedTunnel } = await import(
    "../modules/link-transport/src"
  );
  return startPinnedTunnel(key, host, port, pin, mode);
}

async function stopNativePinnedTunnel(key: string): Promise<void> {
  const { stopPinnedTunnel } = await import(
    "../modules/link-transport/src"
  );
  return stopPinnedTunnel(key);
}

function linkCandidates(server: StoredServer): StoredTransportCandidate[] {
  const configured = (server.transportCandidates || []).filter(
    (candidate) => candidate.kind === "link",
  );
  if (configured.length > 0) {
    return configured;
  }
  return [{ name: "Mewla Link", kind: "link", url: server.url }];
}

function requireTLSServerURL(value: string): URL {
  const parsed = new URL(value);
  if (parsed.protocol !== "wss:") {
    throw new Error("Mewla Link requires a wss:// candidate.");
  }
  return parsed;
}

function rewriteSelection(server: StoredServer, selection: Selection): string {
  // A direct address only ever reaches the phone's own loopback tunnel, on the
  // request path the caller asked for.
  if (selection.path !== "relay") {
    return rewriteURL(server.url, selection.localOrigin);
  }
  const requested = new URL(server.url);
  const selected = new URL(selection.remoteURL);
  return requested.origin === selected.origin
    ? rewriteURL(server.url, selection.localOrigin)
    : rewriteURL(selection.remoteURL, selection.localOrigin);
}

function sleep(ms: number): Promise<null> {
  return new Promise((resolve) => setTimeout(() => resolve(null), ms));
}

function rewriteURL(remoteURL: string, localOrigin: string): string {
  const remote = new URL(remoteURL);
  const local = new URL(localOrigin);
  remote.protocol = local.protocol;
  remote.hostname = local.hostname;
  remote.port = local.port;
  return remote.toString();
}
