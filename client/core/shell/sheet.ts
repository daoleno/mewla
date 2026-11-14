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
 * New session's scroll (its agents and folders) in the room the sheet leaves: 520 at most, what is left beside the
 * fields and Start pinned under it, never negative. Start never goes into the scroll: on the iPhone Duo's cover screen
 * held sideways with the keyboard up, a Start at the scroll's end sat behind the keyboard and the scroll ended in
 * blank (NSH insets a scroll holding the focused field for the keyboard before the sheet rises: Mac re-run 11-14, 1c).
 */
export function scrollBesidePinned(room: number, pinned: number, max = 520): number {
  return Math.max(0, Math.min(max, room - pinned));
}

/**
 * While the keyboard is up for one of New session's fields, the sheet is that field and Start: its agents, folders
 * and other fields step aside until the keyboard goes down, so the field and Start show together on any window
 * (a phone held sideways, the Duo's cover screen) without scrolling.
 */
export function composing(keyboard: number, focused: string | null): boolean {
  return keyboard > 0 && focused !== null;
}

/**
 * Whether New session's Task field shows its "Task" caption: not while it composes, nor on a short window (a phone
 * held sideways, the Duo's cover screen on its side), where its 24 pt go to the agents' scroll and the placeholder
 * ("What should it do?") says it already. By the window alone, so the scroll it frees never brings it back.
 */
export function taskCaptionShows(window: number, composing: boolean): boolean {
  return !composing && window >= SHEET.shortWindow;
}

/** The agent grid's columns: three on a phone (SPEC §15), as many 88 pt tiles as fit on a wide sheet (fewer rows: the folders show). */
export function agentGridColumns(width: number, count: number): number {
  if (width < 520) return 3;
  return Math.max(3, Math.min(count, Math.floor(width / 88)));
}

/**
 * On wide a sheet is a centred dialog (SPEC §14.2): at most 88 % of the window less its top and bottom safe areas
 * (`insets`: a status bar, an unfolded foldable's taskbar), and with the software keyboard up (an open foldable or a
 * tablet, touch) it sits in what the keyboard leaves, 16 pt clear of the keyboard and the top. A dialog the keyboard
 * covered hid its own field and New session's Start on an iPhone Duo held open (669 pt high, a 264 pt keyboard).
 * `lift` is the space the dialog keeps under it; `max` its height, which AppSheet sets on the dialog and the body's
 * room is measured against: one number, so the content never runs past the dialog's rounded bottom (a 7.6" Android
 * foldable unfolded, 11-14: the dialog's own 88 % box was shorter than the room, and its corner clipped Start).
 */
export function dialogFrame(window: number, keyboard: number, insets = 0): { lift: number; max: number } {
  if (keyboard <= 0) return { lift: 0, max: Math.floor(Math.max(0, window - insets) * SHEET.maxShare) };
  return { lift: keyboard + SHEET.keyboardGap, max: Math.max(0, window - keyboard - 2 * SHEET.keyboardGap) };
}

/** The dialog's foot under its content (kit/Sheet.tsx on wide): the 16 its content keeps at the sides. */
export const DIALOG_FOOT = 16;

/**
 * A dialog's body room: its height less its header (its top padding and title, as laid out) and its foot. Never
 * negative.
 */
export function dialogBodyRoom(input: { window: number; keyboard: number; header: number; insets?: number }): number {
  return Math.max(0, dialogFrame(input.window, input.keyboard, input.insets).max - input.header - DIALOG_FOOT);
}
