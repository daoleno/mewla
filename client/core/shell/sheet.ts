/**
 * The Soft sheet's height (kit/Sheet.tsx, soft-final/SPEC.md §11 Sheet): at most 88 % of the window, lifted over
 * the software keyboard by the keyboard's height and 16 more (shell/AppSheet.tsx). A body that scrolls is held to
 * what that leaves, so the controls under it (New session's Start) stay inside the sheet: a fixed 520 pt cap put
 * Start below the screen of an iPhone Duo held sideways (669 and 466 pt high), where nothing could reach it.
 */
export const SHEET = {
  /** Of the window's height. */
  maxShare: 0.88,
  /** Between the keyboard's top and the sheet's content (AppSheet's `aboveKeyboard`). */
  keyboardGap: 16,
  /** The content's foot: the home indicator's room without a keyboard, a small gap over it. */
  foot: 32,
  footOverKeyboard: 8,
  /** A window this short with the keyboard up gives the sheet's title row to the body. */
  shortWindow: 500,
} as const;

/** The space under the sheet's content: 32, or 8 over the keyboard (whose own gap is padding already). */
export function sheetFoot(keyboard: number): number {
  return keyboard > 0 ? SHEET.footOverKeyboard : SHEET.foot;
}

/** Whether the sheet drops its title row: the keyboard is up in a short window (a phone held sideways). */
export function sheetTitleGivesWay(window: number, keyboard: number): boolean {
  return keyboard > 0 && window < SHEET.shortWindow;
}

/**
 * The height a sheet's body may take: the sheet's cap less the keyboard's lift, the sheet's header (grabber and
 * title, as laid out) and its foot. Never negative.
 */
export function sheetBodyRoom(input: { window: number; keyboard: number; header: number }): number {
  const cap = Math.floor(input.window * SHEET.maxShare);
  const lift = input.keyboard > 0 ? input.keyboard + SHEET.keyboardGap : 0;
  return Math.max(0, cap - lift - input.header - sheetFoot(input.keyboard));
}

/**
 * A body that scrolls above a fixed control (New session's Start under its agents and folders): the scroll's
 * height, and whether the control has to go inside the scroll, at its end, because the room left beside it is
 * less than `minScroll` (a short window with the keyboard up). Inside, scrolling reaches it.
 */
export function scrollAboveControl(room: number, control: number, options: { max: number; minScroll: number }): { scroll: number; controlInside: boolean } {
  const beside = Math.min(options.max, room - control);
  if (beside >= options.minScroll) return { scroll: beside, controlInside: false };
  return { scroll: Math.max(0, Math.min(options.max, room)), controlInside: true };
}

/**
 * On wide a sheet is a centred dialog (SPEC §14.2): at most 88 % of the window, and with the software keyboard up
 * (an open foldable or a tablet, touch) it sits in what the keyboard leaves, 16 pt clear of the keyboard and the top.
 * A dialog the keyboard covered hid its own field and New session's Start on an iPhone Duo held open (669 pt high,
 * a 264 pt keyboard). `lift` is the space the dialog keeps under it; `max` its height.
 */
export function dialogFrame(window: number, keyboard: number): { lift: number; max: number } {
  if (keyboard <= 0) return { lift: 0, max: Math.floor(window * SHEET.maxShare) };
  return { lift: keyboard + SHEET.keyboardGap, max: Math.max(0, window - keyboard - 2 * SHEET.keyboardGap) };
}

/** The dialog's foot under its content (kit/Sheet.tsx on wide). */
export const DIALOG_FOOT = 24;

/** A dialog's body room: its height less its header (title, as laid out) and its foot. Never negative. */
export function dialogBodyRoom(input: { window: number; keyboard: number; header: number }): number {
  return Math.max(0, dialogFrame(input.window, input.keyboard).max - input.header - DIALOG_FOOT);
}
