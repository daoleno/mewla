import { motion, type Curve } from "./tokens";

/**
 * The moves of SPEC §16.3 as data the views play: how long, along which curve, from how far below
 * (`rise`, points) and from which scale. With reduced motion only the opacity fades remain, over
 * `dur.fast`; nothing rises, scales or changes size (`size: false`: the height jumps at once).
 */
export interface Move {
  duration: number;
  easing: Curve | "linear";
  /** Points it travels up from (arriving) or toward (leaving). */
  rise: number;
  /** The scale it starts from (arriving) or ends at (leaving). */
  scale: number;
  /** Whether its height or place animates (a fold, a row inserted or removed). */
  size: boolean;
}

const move = (duration: number, easing: Curve, rest: Partial<Move> = {}): Move => ({ duration, easing, rise: 0, scale: 1, size: false, ...rest });

export const moves = {
  /** A message or a row arriving in view: rises 8 pt with its fade. Its height opens first (`open`). */
  arrive: move(motion.dur.base, motion.ease.enter, { rise: 8 }),
  /** A row's height opening or closing, a fold from its chevron, a card closing to its line, a reorder. */
  open: move(motion.dur.move, motion.ease.move, { size: true }),
  /** Leaving: a fade, then the height closes (`open`). */
  leave: move(motion.dur.fast, motion.ease.exit),
  /** A swap in one cell (placeholder → content, state words): the new layer fades in, the old one out. */
  swapIn: move(motion.dur.base, motion.ease.enter),
  swapOut: move(motion.dur.fast, motion.ease.exit),
  /** The jump control and a toast: rise 8 pt (the jump control from .94 scale). */
  jumpIn: move(motion.dur.base, motion.ease.enter, { rise: 8, scale: 0.94 }),
  toastIn: move(motion.dur.base, motion.ease.enter, { rise: 8 }),
  out: move(motion.dur.fast, motion.ease.exit),
  /** The release of a press, colour. */
  release: move(motion.dur.fast, motion.ease.exit),
  /** A switch. */
  toggle: move(motion.dur.base, motion.ease.enter),
  /** A meter's fill. */
  meter: move(motion.dur.move, motion.ease.move, { size: true }),
} as const satisfies Record<string, Move>;

export type MoveName = keyof typeof moves;

/** The move as played: with reduced motion, only a fade over `dur.fast`. */
export function play(name: MoveName | Move, reduce: boolean): Move {
  const m = typeof name === "string" ? moves[name] : name;
  if (!reduce) return m;
  return { duration: motion.dur.fast, easing: "linear", rise: 0, scale: 1, size: false };
}

/** Whether a decorative loop runs (the slow line's pass): never with reduced motion. The spinner is information and always turns. */
export function loops(kind: "slide" | "spin", reduce: boolean): boolean {
  return kind === "spin" || !reduce;
}

/** The spring after a finger lets go, from its velocity; with reduced motion a cut (null: set the value at once). */
export function settle(velocity: number, reduce: boolean): (typeof motion.spring & { velocity: number }) | null {
  return reduce ? null : { ...motion.spring, velocity };
}
