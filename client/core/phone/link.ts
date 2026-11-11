import { normalizeDaemonId } from "../deviceAuthContract";
import type { StoredServer } from "../storedServerContract";

/**
 * The phone's signed WebSocket to its computer (`/phone/device`, daemon/server/phone.go). It lives in
 * the Android host's foreground service (client/android/phone link/PhoneLink.kt), not here: NSH runs
 * no JavaScript once the app is swiped away, and Phone use must survive that and a reboot. The app
 * only says which computer it follows. The frames mirror daemon/phone/protocol.go.
 */
export const PHONE_PROTOCOL_VERSION = 1;

/** The daemon's /phone/device close codes: another phone took Phone use, this protocol is unknown, a bad frame. */
export const PHONE_CLOSE = { replaced: 4001, unsupported: 4002, protocol: 4003 } as const;

/** How long the link waits before trying the computer again (link/PhoneFrames.kt LinkBackoff). */
export const PHONE_LINK_BACKOFF_MS = [1_000, 2_000, 4_000, 8_000, 15_000] as const;

/** What the host's link is told to follow (`phone.follow`). */
export interface PhoneLinkTarget {
  serverId: string;
  /** `ws(s)://<computer>/phone/device`. */
  url: string;
  /** The computer's id the signature names. */
  daemonId: string;
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

/** The one current computer as the link's target; null when there is none or it has no address or id. */
export function phoneLinkTarget(server: Pick<StoredServer, "id" | "url" | "daemonId"> | null): PhoneLinkTarget | null {
  if (!server) return null;
  const url = phoneDeviceURL(server.url);
  const daemonId = normalizeDaemonId(server.daemonId);
  if (!url || !daemonId) return null;
  return { serverId: server.id, url, daemonId };
}

/** Whether two targets are the same computer at the same address: the link is kept then. */
export function sameTarget(a: PhoneLinkTarget | null, b: PhoneLinkTarget | null): boolean {
  if (!a || !b) return a === b;
  return a.serverId === b.serverId && a.url === b.url && a.daemonId === b.daemonId;
}

/**
 * The link Phone use's notifications and the end of pairing open (mewla://phone). Matched as text:
 * the URL class on a phone does not implement every part of a URL.
 */
export function isPhoneLink(url: string): boolean {
  return /^mewla:\/\/phone\/?(?:[?#].*)?$/.test(url.trim());
}
