/**
 * The cold-start hand-off from the native splash: the seal on the splash is
 * the seal the cat sleeps in, so the face fades into the sleeping cat, the cat
 * wakes and hops out, and the app shows. Nothing here holds the app back: the
 * exit starts as soon as the app is ready and the cat is out of the seal, so a
 * fast start is over in about a second and a slow one keeps the cat company
 * until the app is ready.
 */

/** Where the app goes once it starts: pairing for a new phone, else the app. */
export type LaunchDestination = "onboarding" | "app";

export interface LaunchTiming {
  /** The splash face fades into the sleeping cat. */
  crossfadeMs: number;
  /** By now the waking clip has the cat leaping out of the seal. */
  outOfSealMs: number;
  /** The cat moves to its place and the veil fades. */
  exitMs: number;
  /** Without motion the splash only fades. */
  reducedExitMs: number;
}

export const LAUNCH: LaunchTiming = { crossfadeMs: 180, outOfSealMs: 760, exitMs: 320, reducedExitMs: 150 };

/**
 * Milliseconds after the waking clip first shows (or, without motion, after
 * the veil first shows) when the exit starts, or null while the app is still
 * loading. `readyAt` is on the same clock and may be negative when the app
 * was ready before the clip showed.
 */
export function launchExitAt(
  { readyAt, reducedMotion }: { readyAt: number | null; reducedMotion: boolean },
  timing: LaunchTiming = LAUNCH,
): number | null {
  if (readyAt === null) return null;
  if (reducedMotion) return Math.max(0, readyAt);
  return Math.max(readyAt, timing.outOfSealMs);
}

export interface CatBox {
  /** Centre of the cat's square box, in points from the screen's top left. */
  cx: number;
  cy: number;
  /** Side of the box: the 256 px canvas every standing clip shares. */
  size: number;
}

/** The splash image's side in points (app.base.json, expo-splash-screen imageWidth). */
export const SPLASH_IMAGE_SIDE = 128;

/**
 * The cat box whose seal sits exactly on the splash seal. The splash draws
 * the seal 352 px across in its 512 px image; a clip draws it 227 px across
 * in its 256 px canvas, 11 px above the canvas centre.
 */
export function splashSealBox(width: number, height: number): CatBox {
  const size = (SPLASH_IMAGE_SIDE * 352) / 512 * (256 / 227);
  return { cx: width / 2, cy: height / 2 + (11 / 256) * size, size };
}

/**
 * The empty seal drawn behind a cat standing in `box`: same size, nudged up
 * and left so the cat stands in front of it the way the waking clip ends.
 */
export function sealBehind(box: CatBox): CatBox {
  return { cx: box.cx - (7 / 256) * box.size, cy: box.cy - (12 / 256) * box.size, size: box.size };
}
