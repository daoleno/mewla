/**
 * How much room a window has, by its size alone. Never branch on device,
 * idiom or orientation: a foldable's inner screen, a tablet and a resized
 * window all fall out of these numbers (retune them here, after measuring).
 */

/** Window height (pt) below which the window is compact: a phone held sideways (~430). */
export const ROOMY_MIN_HEIGHT = 500;

/** Window width (pt) from which a sidebar and a page fit side by side. */
export const SIDEBAR_DOCK_MIN_WIDTH = 640;

/** Width (pt) of the docked sidebar; the page keeps the rest. */
export const SIDEBAR_WIDTH = 264;

/** Window width (pt) from which Brain adds its Work column beside the chat. */
export const BRAIN_WORK_COLUMN_MIN_WIDTH = 1024;

/** A window tall enough for chrome and content: not a phone held sideways. */
export function hasRoomyHeight(height: number): boolean {
  return height >= ROOMY_MIN_HEIGHT;
}

/** Room for a sidebar and a page: wide and tall. */
export function shouldDockSidebar(width: number, height: number): boolean {
  return width >= SIDEBAR_DOCK_MIN_WIDTH && hasRoomyHeight(height);
}

/** Room for Brain's Work column, which sits beside the docked sidebar. */
export function shouldShowBrainWorkColumn(width: number, height: number): boolean {
  return width >= BRAIN_WORK_COLUMN_MIN_WIDTH && shouldDockSidebar(width, height);
}
