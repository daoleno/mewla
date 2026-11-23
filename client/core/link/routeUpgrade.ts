import { normalizeDaemonId, normalizePublicKeyHex } from "../deviceAuthContract";
import { platform } from "../platform";
import { verifyLinkRouteSignature } from "../protocolCrypto";
import { normalizeServerURL, type StoredServer, type StoredTransportCandidate } from "../storedServerContract";

/**
 * A computer paired by its hosted https address (remote access) is on Mewla Relay all the same: over the connection the
 * app already has, it tells its route (`link_route`: route id, transport key pin, its address on each relay), signed
 * with the daemon key the app pinned at pairing. The app takes the pairing as a Relay route from then on, so it finds
 * the computer's LAN and Tailscale addresses and moves onto them as a fresh Relay pairing does (owner 11-23: nobody
 * pairs again for it). The route is trusted on that signature only, never on the connection it came over: whoever
 * could get a certificate for the hosted name still cannot hand the app another transport key.
 */
export interface LinkRouteChange {
  transportKind: "link";
  transportPin: string;
  linkRouteId: string;
  transportCandidates: StoredTransportCandidate[];
}

/**
 * What the daemon key signs for a route: daemon/link RouteBindingPayload, line for line, or null for a field that holds
 * a control character (a newline would let the same bytes read as another candidate list), which the daemon refuses to
 * sign too. Checked before trimming: Go's TrimSpace strips U+0085, JavaScript's trim keeps it.
 */
export function linkRouteBinding(input: {
  daemonId: string;
  daemonPublicKey: string;
  routeId: string;
  transportPin: string;
  issuedAtMs: number;
  candidates: { name: string; url: string }[];
}): Uint8Array | null {
  const fields = [input.daemonId, input.daemonPublicKey, input.routeId, input.transportPin, ...input.candidates.flatMap((candidate) => [candidate.name, candidate.url])];
  if (fields.some((field) => /\p{Cc}/u.test(field))) return null;
  const lines = [
    "1",
    input.daemonId.trim().toLowerCase(),
    input.daemonPublicKey.trim().toLowerCase(),
    input.routeId.trim().toLowerCase(),
    input.transportPin.trim().toLowerCase(),
    String(input.issuedAtMs),
    ...input.candidates.flatMap((candidate) => [candidate.name.trim(), candidate.url.trim()]),
  ];
  return new TextEncoder().encode(lines.join("\n"));
}

/** How old a signed route may be: it names the computer's own, long-lived key and route. */
const ROUTE_MAX_AGE_MS = 24 * 60 * 60 * 1000;

const hex = (value: unknown, length: number) => (typeof value === "string" && new RegExp(`^[0-9a-f]{${length}}$`).test(value.toLowerCase()) ? value.toLowerCase() : "");

/**
 * What `server` becomes on a `link_route` from its computer, or null to leave it as it is: a pairing that is already a
 * Relay route, a host without pinned tunnels (a browser), a pairing not at one of the route's addresses, or a route
 * that is not the computer's own (a signature that does not verify, a stale one).
 */
export function linkRouteChange(server: StoredServer, frame: Record<string, unknown>): LinkRouteChange | null {
  if (server.transportKind === "link" || !platform().linkTunnels) return null;
  const routeId = hex(frame.route_id, 32);
  const pin = hex(frame.transport_pin, 64);
  const signature = hex(frame.signature, 128);
  const issued = typeof frame.issued_at_ms === "number" && Number.isSafeInteger(frame.issued_at_ms) ? frame.issued_at_ms : 0;
  const daemonId = normalizeDaemonId(server.daemonId);
  const daemonPublicKey = normalizePublicKeyHex(server.daemonPublicKey);
  if (frame.version !== 1 || !routeId || !pin || !signature || !daemonId || !daemonPublicKey) return null;
  if (!issued || Math.abs(Date.now() - issued) > ROUTE_MAX_AGE_MS) return null;
  const listed = Array.isArray(frame.candidates) ? frame.candidates : [];
  const candidates: { name: string; url: string }[] = [];
  for (const item of listed.slice(0, 16)) {
    const { name, url } = (item ?? {}) as Record<string, unknown>;
    if (typeof name !== "string" || typeof url !== "string" || !name.trim() || !url.startsWith("wss://")) return null;
    candidates.push({ name, url });
  }
  if (!candidates.length) return null;
  const binding = linkRouteBinding({ daemonId, daemonPublicKey, routeId, transportPin: pin, issuedAtMs: issued, candidates });
  if (!binding || !verifyLinkRouteSignature({ daemonPublicKey, bindingPayload: binding, signatureHex: signature })) return null;
  // Only the pairing at one of the route's own addresses (the hosted one): a pairing by a LAN or other address stays.
  const here = normalizeServerURL(server.url);
  if (!here || !candidates.some((candidate) => normalizeServerURL(candidate.url) === here)) return null;
  return {
    transportKind: "link",
    transportPin: pin,
    linkRouteId: routeId,
    transportCandidates: [
      ...candidates.map((candidate) => ({ name: candidate.name, kind: "link" as const, url: normalizeServerURL(candidate.url) || candidate.url })),
      // The LAN and Tailscale addresses it already named stay; the address it was paired by is now a relay candidate.
      ...(server.transportCandidates ?? []).filter((candidate) => candidate.kind === "direct"),
    ],
  };
}

/**
 * `linkRouteChange`, taken only once a tunnel pinned to the route's key reached the computer at the address the app is
 * paired by: a host whose tunnels don't work (an older host binary, a network that blocks them) keeps the pairing it
 * has, which works, rather than one it could not reconnect on.
 */
export async function provenLinkRouteChange(server: StoredServer, frame: Record<string, unknown>): Promise<LinkRouteChange | null> {
  const change = linkRouteChange(server, frame);
  const tunnels = platform().linkTunnels;
  if (!change || !tunnels) return null;
  const address = new URL(server.url);
  const key = `route-check:${server.id}`;
  try {
    await tunnels.start(key, address.hostname, address.port ? Number(address.port) : 443, change.transportPin, "measure");
    return change;
  } catch {
    return null;
  } finally {
    await tunnels.stop(key).catch(() => undefined);
  }
}
