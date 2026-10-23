// The daemon's per-stream telemetry, as the Screen viewer receives it.
// Kept apart from the socket so it needs no React Native.

/** How a request reached the computer, as the daemon names it. */
export type ScreenPath = "relay" | "tailscale" | "lan" | "loopback" | "direct";

const SCREEN_PATHS: readonly string[] = ["relay", "tailscale", "lan", "loopback", "direct"];

/**
 * What the daemon measured of one second of the stream, stage by stage, in
 * milliseconds (medians) and bytes. Numbers only: nothing from the screen.
 */
export interface ScreenStreamStats {
  path: ScreenPath | null;
  updates: number;
  bytes: number;
  dirtyPct: number;
  captureSeq: number;
  captureMs: number;
  encodeMs: number;
  encodeMaxMs: number;
  /** From sending an update to the viewer's fence answer, after it drew. */
  ackMs: number;
  ackMaxMs: number;
  /** The JPEG level in use, 0-9, and the updates allowed unacknowledged. */
  quality: number;
  inFlight: number;
}

const finite = (value: unknown): number => (typeof value === "number" && Number.isFinite(value) ? value : 0);

/** Reads the daemon's `stats` message; null when it isn't one. */
export function parseStreamStats(message: Record<string, unknown>): ScreenStreamStats | null {
  if (message.type !== "stats") return null;
  const path = typeof message.path === "string" && SCREEN_PATHS.includes(message.path) ? (message.path as ScreenPath) : null;
  return {
    path,
    updates: finite(message.updates),
    bytes: finite(message.bytes),
    dirtyPct: finite(message.dirty_pct),
    captureSeq: finite(message.capture_seq),
    captureMs: finite(message.capture_ms),
    encodeMs: finite(message.encode_ms),
    encodeMaxMs: finite(message.encode_max_ms),
    ackMs: finite(message.ack_ms),
    ackMaxMs: finite(message.ack_max_ms),
    quality: finite(message.quality),
    inFlight: finite(message.in_flight),
  };
}
