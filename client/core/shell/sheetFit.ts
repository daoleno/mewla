/**
 * A bottom sheet rises at most 88 % of the window (ui/kit/Sheet `max-h-[88%]`) and, with the software keyboard up,
 * sits above it with 16 to spare (ui/shell/AppSheet `aboveKeyboard`). What the sheet keeps outside its scrolling
 * part (grabber, title, a Start under the list: `chrome`) must fit in that too, or it lands below the screen's edge
 * (a 669 pt Duo inner screen or a 466 pt cover screen on its side: New session's Start was off screen).
 */
export const SHEET_HEIGHT_SHARE = 0.88;
export const SHEET_KEYBOARD_GAP = 16;

/**
 * How tall a sheet's scrolling part may be: never more than `cap` (what it has on a tall phone), never less than
 * `floor`, otherwise what is left of the sheet's room once the keyboard and the chrome are taken.
 */
export function sheetScrollHeight(input: { window: number; keyboard: number; chrome: number; cap: number; floor?: number }): number {
  const keyboard = input.keyboard > 0 ? input.keyboard + SHEET_KEYBOARD_GAP : 0;
  const room = Math.floor(input.window * SHEET_HEIGHT_SHARE - keyboard - input.chrome);
  return Math.max(input.floor ?? 0, Math.min(input.cap, room));
}
