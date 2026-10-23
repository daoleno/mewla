import { normalizeDaemonId, normalizePublicKeyHex, verifyDaemonAssertion } from "./deviceAuthContract";
import type { StoredServer, StoredTransportCandidate } from "./storedServerContract";

/**
 * Where a computer says a phone can reach it without the relay. An address is
 * only a hint: it is dialed through a TLS stream pinned to the computer's
 * transport key, and the device header is sent only after the answerer has
 * signed a fresh challenge with the daemon key.
 */
export type DirectKind = "lan" | "tailscale";

export interface DirectAddress {
  host: string;
  port: number;
  kind: DirectKind;
}

export const HEALTH_CHALLENGE_PURPOSE = "mewla-health-challenge";

const known = new Map<string, DirectAddress[]>();

function ipv4(host: string): [number, number, number, number] | null {
  const match = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(host);
  if (!match) return null;
  const octets = match.slice(1).map(Number);
  if (octets.some((octet, index) => octet > 255 || String(octet) !== match[index + 1])) return null;
  return octets as [number, number, number, number];
}

/**
 * The kind of a private-network IPv4 literal, or null for anything else.
 * Hostnames never qualify: a name can be re-pointed (DNS rebinding) to an
 * address the computer never published.
 */
export function directKind(host: string): DirectKind | null {
  const octets = ipv4(host);
  if (!octets) return null;
  const [a, b] = octets;
  if (a === 100 && b >= 64 && b <= 127) return "tailscale";
  if (a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168)) return "lan";
  return null;
}

/** The addresses in a `direct_addresses` message that are safe to try. */
export function parseDirectAddresses(payload: unknown): DirectAddress[] {
  if (!Array.isArray(payload)) return [];
  const parsed: DirectAddress[] = [];
  for (const item of payload) {
    if (!item || typeof item !== "object") continue;
    const { host, port } = item as Record<string, unknown>;
    if (typeof host !== "string" || typeof port !== "number") continue;
    if (!Number.isInteger(port) || port < 1 || port > 65535) continue;
    const kind = directKind(host);
    if (!kind) continue;
    if (parsed.some((entry) => entry.host === host && entry.port === port)) continue;
    parsed.push({ host, port, kind });
  }
  return parsed;
}

const KIND_NAMES: Record<DirectKind, string> = { lan: "LAN", tailscale: "Tailscale" };

/** The stored form of a direct address, on the computer's own request path. */
export function directCandidate(address: DirectAddress, requestPath = "/ws"): StoredTransportCandidate {
  return {
    name: KIND_NAMES[address.kind],
    kind: "direct",
    url: `wss://${address.host}:${address.port}${requestPath}`,
  };
}

/** The usable direct addresses saved on a server. */
export function storedDirectAddresses(server: StoredServer): DirectAddress[] {
  const addresses: DirectAddress[] = [];
  for (const candidate of server.transportCandidates ?? []) {
    if (candidate.kind !== "direct") continue;
    try {
      const url = new URL(candidate.url);
      addresses.push(...parseDirectAddresses([{ host: url.hostname, port: Number(url.port) }]));
    } catch {
      // A damaged entry is skipped, never dialed.
    }
  }
  return addresses;
}

/** What this run has learned for a server; falls back to what was saved. */
export function directAddressesFor(server: StoredServer): DirectAddress[] {
  return known.get(server.id) ?? storedDirectAddresses(server);
}

/**
 * Takes the computer's current list as the whole truth. Returns true when it
 * differs from what the phone had, so callers know to save it.
 */
export function learnDirectAddresses(server: StoredServer, payload: unknown): { addresses: DirectAddress[]; changed: boolean } {
  const addresses = parseDirectAddresses(payload);
  const before = directAddressesFor(server);
  known.set(server.id, addresses);
  return { addresses, changed: !sameAddresses(before, addresses) };
}

export function sameAddresses(left: DirectAddress[], right: DirectAddress[]): boolean {
  const key = (address: DirectAddress) => `${address.host}:${address.port}`;
  const a = left.map(key).sort();
  const b = right.map(key).sort();
  return a.length === b.length && a.every((value, index) => value === b[index]);
}

export function forgetDirectAddresses(serverId: string) {
  known.delete(serverId);
}

interface ChallengePayload {
  daemon_id?: unknown;
  daemon_public_key?: unknown;
  assertion_purpose?: unknown;
  assertion_timestamp?: unknown;
  assertion_nonce?: unknown;
  assertion_signature?: unknown;
}

/**
 * True only if `payload` answers `challengeHex`, names the paired daemon, and
 * is signed by its pinned key. A recorded response fails because it answers a
 * different challenge; another daemon fails the key; a daemon that predates
 * challenges ignores it and fails the nonce.
 */
export function verifyHealthChallenge(input: {
  challengeHex: string;
  daemonId: string;
  daemonPublicKey: string;
  payload: unknown;
}): boolean {
  if (!input.payload || typeof input.payload !== "object") return false;
  const payload = input.payload as ChallengePayload;
  const daemonId = normalizeDaemonId(input.daemonId);
  const daemonPublicKey = normalizePublicKeyHex(input.daemonPublicKey);
  const challenge = input.challengeHex.trim().toLowerCase();
  if (!daemonId || !daemonPublicKey || !/^[0-9a-f]{32}$/.test(challenge)) return false;
  const text = (value: unknown) => (typeof value === "string" ? value : "");
  if (text(payload.assertion_purpose) !== HEALTH_CHALLENGE_PURPOSE) return false;
  if (text(payload.assertion_nonce).trim().toLowerCase() !== challenge) return false;
  if (normalizeDaemonId(text(payload.daemon_id)) !== daemonId) return false;
  if (normalizePublicKeyHex(text(payload.daemon_public_key)) !== daemonPublicKey) return false;
  return verifyDaemonAssertion({
    purpose: HEALTH_CHALLENGE_PURPOSE,
    daemonId,
    daemonPublicKey,
    timestamp: text(payload.assertion_timestamp),
    nonceHex: challenge,
    signatureHex: text(payload.assertion_signature),
  });
}
