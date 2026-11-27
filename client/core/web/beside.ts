/**
 * Beside (client/DESIGN.md "The wide layout", 11-26, 11-27): on a desktop or browser window, what is opened stands
 * beside Brain's thread instead of over it. Two kinds of split:
 * - the board (an ultrawide): the right pane is always split; a Session, a Work or a preview opens beside Brain, and
 *   the Work board fills that pane while nothing is open;
 * - the preview (a laptop): Brain's thread has the right pane until a preview (a file a chat names, a Work file, a file
 *   of Brain's workspace) opens; the preview then stands beside the thread, the person keeps reading and typing, and
 *   closing it gives the thread its width back.
 * Pure, so the shell, the URL and the tests agree.
 */

/** From this right pane (points; the window less the left pane) the board split: a 1920 window with any pane width. */
export const BESIDE_MIN_WIDTH = 1480;

/** Brain's pane while the board split is up: its reading column (760, the Mac's 700) and 20 a side. */
export const BESIDE_BRAIN_WIDTH = { web: 800, mac: 740 } as const;

/**
 * The preview split (11-27, the owner: "use the right side for previews"): Brain's thread keeps at least 480 (a
 * phone's column and its margins), the handle between them 8, the preview at least 540 (a PDF page or a page's
 * narrow layout still reads). A new split gives the preview 55 % of the room; the handle moves it in steps of 16.
 */
export const PREVIEW_SPLIT = { thread: 480, handle: 8, preview: 540, share: 0.55, step: 16 } as const;

/** From this right pane a preview opens beside the thread: a 1366 browser window (1046), a Mac window from 1308 with its 280 pane. */
export const PREVIEW_BESIDE_MIN_WIDTH = PREVIEW_SPLIT.thread + PREVIEW_SPLIT.handle + PREVIEW_SPLIT.preview;

/** Where the preview's share of the split is kept on this device (the handle dragged). */
export const PREVIEW_SHARE_KEY = "mewla.wide.previewShare";

/**
 * From this window width a browser is roomy (a 27" QHD, a 34" ultrawide; Brain's review of #89): the thread's type
 * and the composer a step up (ui/kit/density.ts `roomy`), in an 840 reading column and an 880 Brain pane.
 */
export const ROOMY_MIN_WIDTH = 2560;
export const ROOMY_READING_WIDTH = 840;
export const ROOMY_BRAIN_WIDTH = 880;

/** Which split the right pane has: the board (an ultrawide), the preview (a laptop), or none. */
export type BesideMode = "board" | "preview";

type Page = { name: string; file?: unknown };

/** Besides the previews, what opens beside Brain on the board: a Session and a Work. */
const BOARD_PAGES: ReadonlySet<string> = new Set(["session", "workDetail"]);

/** A preview: a file a chat names, a Work file, or one file of Brain's workspace (a Work result's file). Not a folder's list. */
export function isPreview(route: Page | undefined): boolean {
  if (!route) return false;
  if (route.name === "files-workspace") return typeof route.file === "string" && route.file !== "";
  return route.name === "files-preview" || route.name === "workFile";
}

/**
 * The split: a desktop or browser window (never a phone or a tablet's native stack), wide, with a right pane of 1480
 * or more for the board, of 1028 or more for previews.
 */
export function besideMode(input: { desktop: boolean; wide: boolean; rightPane: number }): BesideMode | null {
  if (!input.desktop || !input.wide) return null;
  if (input.rightPane >= BESIDE_MIN_WIDTH) return "board";
  if (input.rightPane >= PREVIEW_BESIDE_MIN_WIDTH) return "preview";
  return null;
}

/** Whether a page opens beside Brain in this split: on the board a Session, a Work or a preview; on a laptop a preview. Everything else (Settings, Calendar, the computer page) covers both. */
export function opensBeside(route: Page | undefined, mode: BesideMode | null = "board"): boolean {
  if (!route || !mode) return false;
  return isPreview(route) || (mode === "board" && BOARD_PAGES.has(route.name));
}

/** Pages opened together stand beside Brain: on the board when the first does (a Work › its Session), on a laptop when every one is a preview. */
export function allBeside(routes: readonly Page[], mode: BesideMode | null): boolean {
  if (!routes.length || !mode) return false;
  return mode === "board" ? opensBeside(routes[0], mode) : routes.every(isPreview);
}

/** The beside pane shows: the split is up and Brain is the page on the stack; on a laptop only while a preview is open. */
export function besideShows(mode: BesideMode | null, mainLength: number, besideLength: number): boolean {
  if (!mode || mainLength !== 1) return false;
  return mode === "board" || besideLength > 0;
}

/**
 * Crossing into the split: the pages above Brain move into the beside pane when they open beside (Brain › a Session ›
 * what it pushed; on a laptop only previews); null when nothing moves (Brain alone, a page that covers both).
 */
export function intoBeside<R extends Page>(main: readonly R[], mode: BesideMode = "board"): { main: R[]; beside: R[] } | null {
  if (main.length < 2 || !allBeside(main.slice(1), mode)) return null;
  return { main: [main[0]!], beside: main.slice(1) };
}

/**
 * Leaving the split: what the beside pane holds goes back on the one stack above Brain, as a phone would have it;
 * null when it holds nothing. A page already over Brain (Settings) keeps the stack: the beside pages close.
 */
export function outOfBeside<R extends Page>(main: readonly R[], beside: readonly R[]): R[] | null {
  if (beside.length === 0) return null;
  if (main.length > 1) return null;
  return [main[0]!, ...beside];
}

/**
 * The window crosses a gate (a resize, the left pane's handle): the stack over Brain and the beside pages after it.
 * What stands beside stays where it still may (a preview in either split); a Session or a Work leaving the board goes
 * on the stack over Brain; pages over Brain move beside when the new split takes them.
 */
export function crossBeside<R extends Page>(to: BesideMode | null, main: readonly R[], beside: readonly R[]): { main: R[]; beside: R[] } {
  if (to && beside.length > 0 && allBeside(beside, to)) return { main: [...main], beside: [...beside] };
  const stack = outOfBeside(main, beside) ?? [...main];
  if (!to) return { main: stack, beside: [] };
  return intoBeside(stack, to) ?? { main: stack, beside: [] };
}

/** The same place: a preview opened again is not a second entry. */
export function samePage(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

/**
 * What the beside pane holds once `opened` opens in it from Brain, a card, a row or the palette: a preview over a
 * preview goes on top, so Back returns to the first (the same one again changes nothing); anything else replaces what
 * stands there.
 */
export function openedBeside<R extends Page>(beside: readonly R[], opened: readonly R[]): R[] {
  const top = beside.at(-1);
  if (opened.length === 1 && top && isPreview(top) && isPreview(opened[0])) {
    return samePage(top, opened[0]) ? [...beside] : [...beside, opened[0]!];
  }
  return [...opened];
}

/** The routes a URL, the tab title and the left pane's selection read: Brain › what stands beside it, or the stack. */
export function shownRoutes<R extends Page>(main: readonly R[], beside: readonly R[], split: boolean): readonly R[] {
  return split && main.length === 1 && beside.length > 0 ? [main[0]!, ...beside] : main;
}

/**
 * The preview split's two widths in a right pane of `pane` (less a docked screen's strip): the preview takes `share`
 * of the room, never less than 540 or more than leaves the thread 480; a pane too small for both is the thread's
 * minimum and what is left.
 */
export function previewSplit(pane: number, share: number): { thread: number; preview: number } {
  const room = Math.max(0, pane - PREVIEW_SPLIT.handle);
  const most = Math.max(0, room - PREVIEW_SPLIT.thread);
  const preview = Math.round(Math.min(most, Math.max(PREVIEW_SPLIT.preview, room * clampShare(share))));
  return { thread: room - preview, preview };
}

/** A share kept or dragged to: between 10 % and 90 % (the widths' own limits apply at layout); the default when it is not a number. */
export function clampShare(share: number): number {
  if (!Number.isFinite(share)) return PREVIEW_SPLIT.share;
  return Math.min(0.9, Math.max(0.1, share));
}

/** The share a saved value gives: the default when nothing (or nothing numeric) was saved. */
export function storedShare(raw: string | null | undefined): number {
  const value = raw == null || raw.trim() === "" ? NaN : Number(raw);
  return clampShare(value);
}

/** The share a preview `width` makes of a right pane of `pane`, held to the widths the split allows (the handle dragged or stepped). */
export function shareOf(pane: number, width: number): number {
  const room = pane - PREVIEW_SPLIT.handle;
  if (room <= 0) return PREVIEW_SPLIT.share;
  const most = Math.max(PREVIEW_SPLIT.preview, room - PREVIEW_SPLIT.thread);
  const held = Math.min(most, Math.max(PREVIEW_SPLIT.preview, width));
  return clampShare(Math.round((held / room) * 1000) / 1000);
}

/** From this page width a Session shows its chat and its terminal side by side (an ultrawide's beside pane). */
export const SESSION_DUO_MIN_WIDTH = 1600;

/** A Session with a chat, on a desktop or browser window, on a page of 1600 or more: chat and terminal side by side. */
export function showsSessionDuo(input: { desktop: boolean; wide: boolean; chat: boolean; width: number }): boolean {
  return input.desktop && input.wide && input.chat && input.width >= SESSION_DUO_MIN_WIDTH;
}
