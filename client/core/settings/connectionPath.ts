import type { StoredServer } from "../storedServerContract";

/** How the phone reaches the current computer, as words and a mark (the Expo app's names). */

export type TransportPath = "relay" | "lan" | "tailscale";

export const PATH_LABELS: Record<TransportPath | "loopback" | "direct", string> = {
  relay: "Relay",
  tailscale: "Tailscale",
  lan: "LAN",
  loopback: "Local",
  direct: "Direct",
};

/** The active path of the connection to a computer. */
export interface TransportPathInfo {
  path: TransportPath;
  /** The computer's address on that path; null on the relay. */
  host: string | null;
  /** The last measured round trip, in ms; null when not measured. */
  rttMs: number | null;
}

/** Direct is the calm normal state; the relay works, only slower. */
export interface PathMark {
  path: TransportPath;
  kind: "direct" | "relay";
  /** "LAN", "Tailscale" or "Relay". */
  label: string;
  /** What a long press reveals: "LAN · 192.168.1.20 · 12 ms". */
  detail: string;
}

/**
 * The mark for a computer's row, or null when there is nothing true to say: no path,
 * or the connection is not up (a path left from a connection that ended is not current).
 */
export function pathMark(info: TransportPathInfo | null, connected: boolean): PathMark | null {
  if (!info || !connected) return null;
  const label = PATH_LABELS[info.path];
  const parts = [label];
  // Only a LAN address is worth reading; a Tailscale one is just a number.
  if (info.path === "lan" && info.host) parts.push(info.host);
  if (info.rttMs !== null && Number.isFinite(info.rttMs) && info.rttMs > 0) parts.push(`${Math.round(info.rttMs)} ms`);
  return { path: info.path, kind: info.path === "relay" ? "relay" : "direct", label, detail: parts.join(" · ") };
}

function ipv4(host: string): [number, number, number, number] | null {
  const match = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(host);
  if (!match) return null;
  const octets = match.slice(1).map(Number);
  if (octets.some((octet, index) => octet > 255 || String(octet) !== match[index + 1])) return null;
  return octets as [number, number, number, number];
}

/**
 * The kind of a private-network IPv4 literal, or null for anything else. Hostnames
 * never qualify: a name can be pointed at an address the computer never published.
 */
export function directKind(host: string): "lan" | "tailscale" | null {
  const octets = ipv4(host);
  if (!octets) return null;
  const [a, b] = octets;
  if (a === 100 && b >= 64 && b <= 127) return "tailscale";
  if (a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168)) return "lan";
  return null;
}

/**
 * The path of a connection that dials `url` for `server`: Mewla Link's relay for a
 * Link computer, LAN or Tailscale for a private address; null for any other address
 * (a public name says nothing about the path, as in the Expo app).
 */
export function pathOfConnection(server: StoredServer, url: string = server.url): TransportPathInfo | null {
  if (server.transportKind === "link") return { path: "relay", host: null, rttMs: null };
  let host = "";
  try {
    host = new URL(url).hostname;
  } catch {
    return null;
  }
  const kind = directKind(host);
  return kind ? { path: kind, host, rttMs: null } : null;
}
