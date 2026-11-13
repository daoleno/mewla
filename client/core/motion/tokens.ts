import { OUTBOX_UNCERTAIN_PATIENCE_MS } from "../conversation/outbox";

/**
 * Waiting and motion (soft-final SPEC §16.1): every wait and every movement in the app reads its numbers
 * from this table, under the SPEC's own names (`motion.wait.quiet`, `motion.dur.move`, `motion.ease.enter`).
 * Times in milliseconds. Curves are cubic Béziers `[x1, y1, x2, y2]`, which NSH takes as an `Easing` as
 * they are (`timing(to, { easing: motion.ease.move })`); `bezier()` writes one for a style transition.
 */
export const motion = {
  wait: {
    /** Until then nothing is drawn except the press. Most answers arrive inside it. */
    quiet: 150,
    /** A placeholder, once drawn, stays at least this long, so it never flashes. */
    hold: 300,
    /** The slow line appears. */
    slow: 3_000,
    /** With the link up and no answer: the place's failed state with Try again (a place with a longer honest bound keeps it). */
    fail: 15_000,
    /** A wait on the person in another app (browser sign-in) ends quietly. */
    person: 600_000,
  },
  link: {
    /** Connecting for longer: the computer is Offline, and stays so between retries. */
    away: 10_000,
    /** The retries' spacing while away (the transport's own backoff: 1 s doubling to 30 s). */
    retries: [1_000, 2_000, 4_000, 8_000, 16_000, 30_000],
  },
  list: {
    /** A list reorders this long after the last touch on it. */
    settle: 1_000,
  },
  /** A toast stays. */
  toast: 4_000,
  turn: {
    /** Brain's turn with no output: the Working line says "No output for 1m". */
    stall: 60_000,
  },
  send: {
    /** A message becomes "Not sent" (the outbox's own limit). While offline it waits in the queue instead. */
    giveup: OUTBOX_UNCERTAIN_PATIENCE_MS,
  },
  dur: {
    /** Leaving, colour, opacity, the release of a press. */
    fast: 120,
    /** Arriving, a swap, the jump control, a toast, state words. */
    base: 200,
    /** Anything that changes size or place: a fold, a card into its line, a row inserted or removed, a reorder. */
    move: 300,
  },
  ease: {
    /** Arriving. */
    enter: [0.2, 0.8, 0.2, 1],
    /** Leaving. */
    exit: [0.4, 0, 1, 1],
    /** In-place size and position. */
    move: [0.4, 0, 0.2, 1],
  },
  /** Settling after a finger lets go (the three pages, a sheet's drag): critically damped, no overshoot. */
  spring: { stiffness: 380, damping: 39, mass: 1 },
  loop: {
    /** One turn of the one spinner. */
    spin: 900,
    /** One pass of the slow line. */
    slide: 1_400,
  },
} as const satisfies {
  ease: Record<string, readonly [number, number, number, number]>;
  [group: string]: unknown;
};

export type Curve = readonly [number, number, number, number];
export type CurveName = keyof typeof motion.ease;

/** A curve as a style transition's timing function: `cubic-bezier(0.2, 0.8, 0.2, 1)`. */
export function bezier(curve: Curve | CurveName): `cubic-bezier(${string})` {
  const c = typeof curve === "string" ? motion.ease[curve] : curve;
  return `cubic-bezier(${c.join(", ")})`;
}

/** The spinner's sizes: in a value's place, and in a pressed button. */
export const RING = { value: 16, button: 20 } as const;
