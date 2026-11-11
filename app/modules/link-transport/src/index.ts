import { requireNativeModule } from "expo-modules-core";

export interface PinnedTunnelResult {
  port: number;
  rttMs: number;
}

export type PinnedTunnelMode = "measure" | "on-demand";

interface LinkTransportNativeModule {
  start(
    key: string,
    host: string,
    port: number,
    spkiSHA256: string,
    mode: PinnedTunnelMode,
  ): Promise<PinnedTunnelResult>;
  stop(key: string): Promise<void>;
  stopAll(): Promise<void>;
  events?(key: string): Promise<string[]>;
}

let cached: LinkTransportNativeModule | null | undefined;

function nativeModule(): LinkTransportNativeModule {
  if (cached === undefined) {
    try {
      cached =
        requireNativeModule<LinkTransportNativeModule>("LinkTransport");
    } catch {
      cached = null;
    }
  }
  if (!cached) {
    throw new Error(
      "Mewla Relay needs a current Android or iOS build. Update/rebuild the app, then scan again.",
    );
  }
  return cached;
}

export function startPinnedTunnel(
  key: string,
  host: string,
  port: number,
  spkiSHA256: string,
  mode: PinnedTunnelMode,
): Promise<PinnedTunnelResult> {
  return nativeModule().start(key, host, port, spkiSHA256, mode);
}

export function stopPinnedTunnel(key: string): Promise<void> {
  return nativeModule().stop(key);
}

export function stopAllPinnedTunnels(): Promise<void> {
  return nativeModule().stopAll();
}

/** What a tunnel saw, for a pairing that did not finish; empty where there is nothing to say. */
export async function pinnedTunnelEvents(key: string): Promise<string[]> {
  try {
    return (await nativeModule().events?.(key)) ?? [];
  } catch {
    return [];
  }
}
