/**
 * When a loading screen is on screen. A load that ends before `delayMs`
 * never shows one; once shown it stays at least `minMs`, then fades for
 * `fadeMs` over whatever arrived. So nothing flashes for 100 ms.
 */
export type LoadingVeilPhase = "waiting" | "shown" | "leaving" | "gone";

export interface LoadingVeilTiming {
  delayMs: number;
  minMs: number;
  fadeMs: number;
}

export const LOADING_VEIL: LoadingVeilTiming = { delayMs: 250, minMs: 600, fadeMs: 220 };

export function loadingVeilPhase(
  {
    startedAt,
    endedAt,
    now,
  }: {
    /** When loading began. */
    startedAt: number;
    /** When it ended; null while it is still loading. */
    endedAt: number | null;
    now: number;
  },
  timing: LoadingVeilTiming = LOADING_VEIL,
): { phase: LoadingVeilPhase; nextAt?: number } {
  const showAt = startedAt + timing.delayMs;
  if (endedAt !== null && endedAt < showAt) return { phase: "gone" };
  if (now < showAt) return { phase: "waiting", nextAt: showAt };
  if (endedAt === null) return { phase: "shown" };
  const leaveAt = Math.max(endedAt, showAt + timing.minMs);
  if (now < leaveAt) return { phase: "shown", nextAt: leaveAt };
  const goneAt = leaveAt + timing.fadeMs;
  if (now < goneAt) return { phase: "leaving", nextAt: goneAt };
  return { phase: "gone" };
}
