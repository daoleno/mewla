/**
 * A long message the person sent folds (SPEC §11 Message, owner 11-13): taller than `foldsAbove` lines of
 * its text, it shows its first `shows` lines, fading out at the bubble's foot, with a chevron that opens
 * and closes it. The one place the numbers live.
 */
export const MESSAGE_FOLD = {
  /** The message text's line in points (`message` in nsh.json: 17 at 1.4706). */
  line: 25,
  /** It folds when its text is taller than this many lines. */
  foldsAbove: 8,
  /** Folded, this many lines show. */
  shows: 6,
} as const;

/** Whether text this tall (points, as laid out) folds. Half a line of slack: a font's rounding never folds 8 lines. */
export function messageFolds(textHeight: number): boolean {
  return textHeight > (MESSAGE_FOLD.foldsAbove + 0.5) * MESSAGE_FOLD.line;
}

/** A folded message's text height: its first lines. */
export const FOLDED_TEXT_HEIGHT = MESSAGE_FOLD.shows * MESSAGE_FOLD.line;
