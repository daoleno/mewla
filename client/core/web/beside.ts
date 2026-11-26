/**
 * Beside (client/DESIGN.md "The wide layout", 11-26): on a wide enough desktop or browser window, what a Work card, a
 * tray row or a link opens (a Session, a Work, a Work file) stands beside Brain's thread instead of over it, and the
 * Work board fills that pane while nothing is open. Pure, so the shell, the URL and the tests agree.
 */

/** From this right pane (points; the window less the left pane) the right pane splits: a 1920 window with any pane width. */
export const BESIDE_MIN_WIDTH = 1480;

/** Brain's pane while the beside pane stands next to it: its reading column (760, the Mac's 700) and 20 a side. */
export const BESIDE_BRAIN_WIDTH = { web: 800, mac: 740 } as const;

/**
 * From this window width a browser is roomy (a 27" QHD, a 34" ultrawide; Brain's review of #89): the thread's type
 * and the composer a step up (ui/kit/density.ts `roomy`), in an 840 reading column and an 880 Brain pane.
 */
export const ROOMY_MIN_WIDTH = 2560;
export const ROOMY_READING_WIDTH = 840;
export const ROOMY_BRAIN_WIDTH = 880;

/** The pages that open beside Brain (a file a chat names: its preview); everything else (Settings, Calendar, the computer page) still covers both. */
const BESIDE_PAGES: ReadonlySet<string> = new Set(["session", "workDetail", "workFile", "files-preview"]);

/** Whether a page opens in the beside pane (when it is up). */
export function opensBeside(route: { name: string } | undefined): boolean {
  return Boolean(route && BESIDE_PAGES.has(route.name));
}

/** The split: a desktop or browser window (never a phone or a tablet's native stack), wide, with a right pane of 1480 or more. */
export function showsBeside(input: { desktop: boolean; wide: boolean; rightPane: number }): boolean {
  return input.desktop && input.wide && input.rightPane >= BESIDE_MIN_WIDTH;
}

/**
 * Crossing into the split: the pages above Brain move into the beside pane when the first opens beside (Brain › a
 * Session › what it pushed); null when nothing moves (Brain alone, or a page that covers both, such as Settings).
 */
export function intoBeside<R extends { name: string }>(main: readonly R[]): { main: R[]; beside: R[] } | null {
  if (main.length < 2 || !opensBeside(main[1])) return null;
  return { main: [main[0]!], beside: main.slice(1) };
}

/**
 * Leaving the split: what the beside pane holds goes back on the one stack above Brain, as a phone would have it;
 * null when it holds nothing. A page already over Brain (Settings) keeps the stack: the beside pages close.
 */
export function outOfBeside<R extends { name: string }>(main: readonly R[], beside: readonly R[]): R[] | null {
  if (beside.length === 0) return null;
  if (main.length > 1) return null;
  return [main[0]!, ...beside];
}

/** The routes a URL, the tab title and the left pane's selection read: Brain › what stands beside it, or the stack. */
export function shownRoutes<R extends { name: string }>(main: readonly R[], beside: readonly R[], split: boolean): readonly R[] {
  return split && main.length === 1 && beside.length > 0 ? [main[0]!, ...beside] : main;
}

/** From this page width a Session shows its chat and its terminal side by side (an ultrawide's beside pane). */
export const SESSION_DUO_MIN_WIDTH = 1600;

/** A Session with a chat, on a desktop or browser window, on a page of 1600 or more: chat and terminal side by side. */
export function showsSessionDuo(input: { desktop: boolean; wide: boolean; chat: boolean; width: number }): boolean {
  return input.desktop && input.wide && input.chat && input.width >= SESSION_DUO_MIN_WIDTH;
}
