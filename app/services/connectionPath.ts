// How the phone reaches the current computer, as words and marks. Pure: the
// path itself comes from pinnedTransport (the connection layer), and Screen's
// details read these same names, so the two never disagree.
import type { ScreenPath } from "./screenStream";

/** How each path is named in the app. */
export const PATH_LABELS: Record<ScreenPath, string> = {
  relay: "Relay",
  tailscale: "Tailscale",
  lan: "LAN",
  loopback: "Local",
  direct: "Direct",
};

/** The active path of the app's connection to a computer. */
export interface TransportPathInfo {
  path: "relay" | "lan" | "tailscale";
  /** The computer's address on that path; null on the relay. */
  host: string | null;
  /** The last measured round trip to it, in ms; null when not measured. */
  rttMs: number | null;
}

/** Direct is the calm normal state; the relay works, only slower. */
export type PathMarkKind = "direct" | "relay";

export interface PathMark {
  path: TransportPathInfo["path"];
  kind: PathMarkKind;
  /** "LAN", "Tailscale" or "Relay". */
  label: string;
  /** What a tap reveals: "LAN · 192.168.1.20 · 12 ms". */
  detail: string;
}

/**
 * The mark for a computer's row, or null when there is nothing true to say:
 * no path chosen yet, or the connection is not up (a path left over from a
 * connection that ended must not read as current).
 */
export function pathMark(info: TransportPathInfo | null, connected: boolean): PathMark | null {
  if (!info || !connected) return null;
  const label = PATH_LABELS[info.path];
  const parts = [label];
  // Only a LAN address is worth reading; a Tailscale one is just a number.
  if (info.path === "lan" && info.host) parts.push(info.host);
  if (info.rttMs !== null && Number.isFinite(info.rttMs) && info.rttMs > 0) parts.push(`${Math.round(info.rttMs)} ms`);
  return {
    path: info.path,
    kind: info.path === "relay" ? "relay" : "direct",
    label,
    detail: parts.join(" · "),
  };
}
