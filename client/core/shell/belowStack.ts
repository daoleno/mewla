/**
 * What lies below the page stack (Android's navigation bar when the app draws edge to edge): the keyboard covers
 * it before it reaches a page, so a composer or the terminal's key bar is lifted by the keyboard less this.
 *
 * It is the window's height less the stack pane's bottom, read from both as they are now. Taken once when the
 * pane laid out, a rotation could pair the new pane with the old window: the Duo's inner screen turned from
 * 669 × 951 to 951 × 669 kept 951 − 669 = 282 "below", which cancelled the 264 pt keyboard, and the terminal's
 * key bar and newest lines stayed under it (audit 11-13, duo2-37/38/54).
 */
export function belowStack(windowHeight: number, paneBottom: number | null): number {
  if (paneBottom === null) return 0;
  return Math.max(0, windowHeight - paneBottom);
}

/** How far a page's foot (a composer, the terminal's key bar) rises over the keyboard. */
export function keyboardLift(keyboard: number, below: number): number {
  return Math.max(0, keyboard - below);
}
