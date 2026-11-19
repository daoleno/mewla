import { normalizeDaemonId } from "../deviceAuthContract";
import { directAddressesFor } from "../link/directAddresses";
import type { StoredServer } from "../storedServerContract";

/**
 * The phone's signed WebSocket to its computer (`/phone/device`, daemon/server/phone.go). It lives in
 * the Android host's foreground service (client/android/phone link/PhoneLink.kt), not here: NSH runs
 * no JavaScript once the app is swiped away, and Phone use must survive that and a reboot. The app
 * only says which computer it follows. The frames mirror daemon/phone/protocol.go.
 */

/** What the host's link is told to follow (`phone.follow`). */
export interface PhoneLinkTarget {
  serverId: string;
  /** `ws(s)://<computer>/phone/device`. */
  url: string;
  /** The computer's id the signature names. */
  daemonId: string;
  /**
   * Mewla Relay: the transport key pin, and the computer's own addresses (`wss://host:port/phone/device`, direct ones
   * first, the relay last) that the link connects to with a pinned TLS of its own. `url` is then the app's loopback
   * tunnel, which the app stops whenever its own connection drops or moves: a host that knows `endpoints` never uses
   * it (Phone use died with every app drop, 11-19); an older host still does.
   */
  pin?: string;
  endpoints?: string[];
}

/** The socket's address on the address the app uses for this computer: ws for http, wss for https. */
export function phoneDeviceURL(serverUrl: string): string | null {
  const trimmed = serverUrl.trim();
  if (!trimmed) return null;
  try {
    const url = new URL(trimmed);
    if (url.protocol === "http:" || url.protocol === "ws:") url.protocol = "ws:";
    else if (url.protocol === "https:" || url.protocol === "wss:") url.protocol = "wss:";
    else return null;
    url.pathname = "/phone/device";
    url.search = "";
    url.hash = "";
    return url.toString();
  } catch {
    return null;
  }
}

type LinkServer = Pick<StoredServer, "id" | "url" | "daemonId"> & Partial<Pick<StoredServer, "transportKind" | "transportPin" | "transportCandidates">>;

/**
 * The one current computer as the link's target; null when there is none or it has no address or id. `urlNow` is the
 * address the app reaches it at now (on Mewla Relay its loopback tunnel), for hosts that know no `endpoints`.
 */
export function phoneLinkTarget(server: LinkServer | null, urlNow?: string): PhoneLinkTarget | null {
  if (!server) return null;
  const url = phoneDeviceURL(urlNow ?? server.url);
  const daemonId = normalizeDaemonId(server.daemonId);
  if (!url || !daemonId) return null;
  const target: PhoneLinkTarget = { serverId: server.id, url, daemonId };
  const pin = server.transportKind === "link" ? (server.transportPin ?? "").toLowerCase() : "";
  if (!/^[0-9a-f]{64}$/.test(pin)) return target;
  const endpoints = linkEndpoints(server as StoredServer);
  return endpoints.length > 0 ? { ...target, pin, endpoints } : target;
}

/** The computer's addresses for the link's own pinned TLS: the direct ones it announced, then its relay candidates. */
function linkEndpoints(server: StoredServer): string[] {
  const direct = directAddressesFor(server).map((address) => `wss://${address.host.includes(":") ? `[${address.host}]` : address.host}:${address.port}/phone/device`);
  const configured = (server.transportCandidates ?? []).filter((candidate) => candidate.kind === "link").map((candidate) => candidate.url);
  const relays = (configured.length > 0 ? configured : [server.url])
    .map((url) => (url.trim().startsWith("wss:") ? phoneDeviceURL(url) : null))
    .filter((url): url is string => url !== null);
  return [...new Set([...direct, ...relays])];
}

/** Whether two targets are the same computer at the same address: the link is kept then. */
export function sameTarget(a: PhoneLinkTarget | null, b: PhoneLinkTarget | null): boolean {
  if (!a || !b) return a === b;
  return (
    a.serverId === b.serverId &&
    a.url === b.url &&
    a.daemonId === b.daemonId &&
    (a.pin ?? "") === (b.pin ?? "") &&
    (a.endpoints ?? []).join("\n") === (b.endpoints ?? []).join("\n")
  );
}

/**
 * The link Phone use's notifications and the end of pairing open (mewla://phone). Matched as text:
 * the URL class on a phone does not implement every part of a URL.
 */
export function isPhoneLink(url: string): boolean {
  return /^mewla:\/\/phone\/?(?:[?#].*)?$/.test(url.trim());
}
