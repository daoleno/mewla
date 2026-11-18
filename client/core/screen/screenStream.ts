// The daemon's per-stream telemetry, as the Screen viewer receives it.
// Kept apart from the socket so it needs no React Native.

/** How a request reached the computer, as the daemon names it. */
export type ScreenPath = "relay" | "tailscale" | "lan" | "loopback" | "direct";

const SCREEN_PATHS: readonly string[] = ["relay", "tailscale", "lan", "loopback", "direct"];

/**
 * What the computer measured of one second of the stream (pushed video over
 * WebRTC), in milliseconds and bytes. Numbers only: nothing from the screen.
 */
export interface ScreenStreamStats {
  /** The media's own route: peer to peer (LAN, Tailscale, Local) or Relay (TURN); null until it is connected. */
  path: ScreenPath | null;
  /** Pictures sent. */
  updates: number;
  bytes: number;
  encodeMs: number;
  encodeMaxMs: number;
  /** The media path's round trip, 0 when unknown. */
  rttMs: number;
  /** The bit rate the stream follows now, kbit/s. */
  targetKbps: number;
}

const finite = (value: unknown): number => (typeof value === "number" && Number.isFinite(value) ? value : 0);

/** Reads the computer's `stats` message; null when it isn't one. */
export function parseStreamStats(message: Record<string, unknown>): ScreenStreamStats | null {
  if (message.type !== "stats") return null;
  const route = message.route;
  const path = typeof route === "string" && SCREEN_PATHS.includes(route) ? (route as ScreenPath) : null;
  return {
    path,
    updates: finite(message.updates),
    bytes: finite(message.bytes),
    encodeMs: finite(message.encode_ms),
    encodeMaxMs: finite(message.encode_max_ms),
    rttMs: finite(message.rtt_ms),
    targetKbps: finite(message.kbps),
  };
}

/** How a route is named in the connection details. */
export const PATH_LABELS: Record<ScreenPath, string> = {
  relay: "Relay",
  tailscale: "Tailscale",
  lan: "LAN",
  loopback: "Local",
  direct: "Direct",
};
