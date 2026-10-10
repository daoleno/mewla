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

