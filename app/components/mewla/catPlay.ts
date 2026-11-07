import type { BrainCatState } from "./brainCatState";

/**
 * Play with the cat. It only answers when addressed: a tap, a second tap
 * close behind (a happy hop), a hold (petting), or a hold then a drag (a
 * small toss that settles back). Nothing starts on its own.
 */

/** A second tap within this window is a double-tap. */
export const DOUBLE_TAP_MS = 300;
/** Holding this long pets the cat; moving after it picks the cat up. */
export const PET_HOLD_MS = 380;

/** The empty bed has no cat to play with; tapping it still pairs. */
export function catCanPlay(state: BrainCatState): boolean {
  return state !== "homeless";
}

export function isDoubleTap(lastTapAt: number | null, now: number): boolean {
  return lastTapAt !== null && now - lastTapAt <= DOUBLE_TAP_MS;
}

/**
 * What a tap on the cat does. Whatever the tap is for (open Work, retry the
 * connection) happens on every tap, a second one included; the cat's own
 * answer is a hop, or a cheer on a double-tap. Only a hold that already petted
 * the cat answers instead of the tap.
 */
export function catTapOutcome({
  consumed,
  cheered,
}: {
  /** A hold or a carry already answered this press. */
  consumed: boolean;
  /** This tap was the second of a double-tap and the cat cheered. */
  cheered: boolean;
}): { press: boolean; hop: boolean } {
  if (consumed) return { press: false, hop: false };
  return { press: true, hop: !cheered };
}

/**
 * Where the held cat sits for a drag of `delta`: it follows the finger, then
 * eases to a stop at `reach`, so the toss stays within the cat's own area.
 */
export function tossOffset(delta: number, reach: number): number {
  if (reach <= 0) return 0;
  return reach * Math.tanh(delta / reach);
}

/** The held cat leans with the drag, up to a small tilt in degrees. */
export function tossTilt(velocityX: number): number {
  return Math.max(-12, Math.min(12, velocityX / 80));
}

