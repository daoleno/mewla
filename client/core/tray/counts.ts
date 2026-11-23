/**
 * The tray's title counts on one line ("1 needs you · 1 new · 3 running"): which of their forms fits the room they
 * have. The words never wrap and are never cut; when the line is too wide, "needs you" gives way to its dot and number
 * first, then "running" to its own ("● 3"), the way the collapsed state control reads. "New" has no dot and keeps
 * its word.
 */
export type CountKind = "needs" | "fresh" | "running";

/** 0: every count in words; 1: needs you as its dot and number; 2: running as well. */
export type CountsForm = 0 | 1 | 2;

/** The widest a character of the title's semibold text is, as a share of its size: wide on purpose, a miss only shortens. */
const CHAR = 0.56;
/** A count's button: the padding either side, and the dot with its gap before the number when it has one. */
const PAD = 12;
const GAP = 6;
/** Between two counts. */
const BETWEEN = 4;

export type CountPart = { kind: CountKind; n: number; words: string };

/** Whether a count in this form is its dot and number rather than its words. */
export function isShort(kind: CountKind, form: CountsForm): boolean {
  return kind === "needs" ? form >= 1 : kind === "running" && form >= 2;
}

/** Whether a count draws a dot before its text: needs you always, running when it is short. */
export function hasDot(kind: CountKind, form: CountsForm): boolean {
  return kind === "needs" || (kind === "running" && form >= 2);
}

/** The text a count shows in this form. */
export function countText(part: CountPart, form: CountsForm): string {
  return isShort(part.kind, form) ? String(part.n) : part.words;
}

/** The width of the counts in a form, at `size` (the text's point size) and `dot` (the dot's). */
export function countsWidth(parts: readonly CountPart[], form: CountsForm, size: number, dot: number): number {
  const widths = parts.map((part) => PAD + (hasDot(part.kind, form) ? dot + GAP : 0) + countText(part, form).length * size * CHAR);
  return widths.reduce((sum, width) => sum + width, 0) + Math.max(0, parts.length - 1) * BETWEEN;
}

/** The fullest form that fits `room` points (unknown before the first layout: the fullest); the shortest when none does. */
export function countsForm(parts: readonly CountPart[], room: number | null, size: number, dot: number): CountsForm {
  if (room === null) return 0;
  for (const form of [0, 1] as const) if (countsWidth(parts, form, size, dot) <= room) return form;
  return 2;
}
