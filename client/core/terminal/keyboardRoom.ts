/**
 * How the terminal group sits over the software keyboard on a short window (SPEC 11 "Terminal group").
 *
 * The group is padded 14 above and 20 below its text. On a phone on its side the keyboard leaves little
 * else: an iPhone SE in landscape kept a 49 pt group, 15 pt of text under that padding, so the native view's
 * two-row minimum put the prompt's row below the group's edge and typed text did not show (audit 11-13,
 * se-37/38/39; an iPhone 18 Pro on its side kept 58). While the keyboard leaves fewer rows than `ROOM_ROWS`,
 * the group's padding gives way to `TIGHT_PADDING` and the key bar's to 4 and 4, which the rows take.
 */
export const GROUP_PADDING = { top: 14, right: 12, bottom: 20, left: 12 };
export const TIGHT_PADDING = { top: 4, right: 12, bottom: 4, left: 12 };

/** The terminal's line height (mono 10/14, fontSize.ts) and the rows a typed command needs to be read with its answer. */
export const ROW_HEIGHT = 14;
export const ROOM_ROWS = 6;

/** The group's text height with the usual padding: the group's height less its padding. */
export function groupTextHeight(groupHeight: number): number {
  return groupHeight - GROUP_PADDING.top - GROUP_PADDING.bottom;
}

/**
 * Whether the group is tight: the keyboard is up and the usual padding would leave fewer than `ROOM_ROWS` rows.
 * `room` is the height the group has between the page's top inset and the key bar over the keyboard.
 */
export function terminalTight(input: { keyboard: number; room: number }): boolean {
  return input.keyboard > 0 && groupTextHeight(input.room) < ROOM_ROWS * ROW_HEIGHT;
}

/** The group's padding: the usual, or `TIGHT_PADDING` while tight. */
export function terminalGroupPadding(tight: boolean): typeof GROUP_PADDING {
  return tight ? TIGHT_PADDING : GROUP_PADDING;
}

/** The key bar's space above its keys and, over the keyboard, below them: 8 and 10, or 4 and 4 while tight. */
export function keyBarPadding(tight: boolean): { top: number; bottom: number } {
  return tight ? { top: 4, bottom: 4 } : { top: 8, bottom: 10 };
}

/** Rows the group shows: `room` measured with the usual padding (`terminalTight`), given back by a tight one. */
export function terminalRows(room: number, tight: boolean): number {
  const bar = keyBarPadding(false).top + keyBarPadding(false).bottom - keyBarPadding(tight).top - keyBarPadding(tight).bottom;
  const pad = terminalGroupPadding(tight);
  return Math.floor((room + bar - pad.top - pad.bottom) / ROW_HEIGHT);
}
