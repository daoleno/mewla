/** From the Expo app's components/ui/workStatus.ts (the pure part: states, glyphs, labels, inks). */
/**
 * Seal & Slip Work states. Each state owns one glyph shape, so the six stay
 * apart in greyscale and for colour-blind readers; colour only reinforces it.
 * "Needs you" is the seal (vermilion) and is the one loud state.
 */
export type WorkStatus =
  | "ready"
  | "running"
  | "needs"
  | "warning"
  | "failed"
  | "blocked";
