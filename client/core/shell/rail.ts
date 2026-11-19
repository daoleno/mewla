/**
 * The side rail (client/DESIGN.md "The side rail"): on an iPhone that keeps a vertical bar at one side (the iPhone
 * Duo's 84 pt bar: its cover screen either way up, its inner screen on its side), Mewla's buttons sit in that bar,
 * icons only, 44 pt each, under the system's own items at its top (the Island, the time, the battery).
 *
 * Top group: the page's own controls, moved out of its header (Back, Stop while a turn runs, the page's actions).
 * Bottom group: the app's fixed places (`RAIL_PLACES`), pinned to the bar's foot. They are shortcuts to pages that
 * already exist, not tabs: the stack and its routes stay as they are. What does not fit goes, from the bottom of
 * the order up, into one "More" menu in the last slot (Apple's rule for the bar).
 *
 * Every number the rail is drawn with is here. Sizes in points.
 */
export const RAIL = {
  /** A side safe-area inset at least this wide, on one side only, is the system's bar (the Duo's is 84). An iPhone's
   * cutout on its side is about 62 on both sides, and never a rail. */
  minBar: 80,
  /** Each button: a 44 target, centred across the bar. */
  target: 44,
  /** Its glyph. */
  glyph: 22,
  /**
   * The bar's end the system keeps: the Island and the status items, where a tap opens Control Center. They sit at
   * the camera's end of the bar. Measured on the Duo simulator (iOS 27.2, 11-19, LLDB: the active occlusion region
   * and the bar's layout guide):
   * - cover screen upright (466×678): the top 174;
   * - cover screen on its side (678×466): the bottom 82 (the camera's end), the top free from y 2;
   * - inner screen on its side (951×669): the top 124.
   * Each with a margin. NSH reports neither the region nor the guide, so a Live Activity growing the Island down the
   * bar is not followed.
   */
  systemTopUpright: 180,
  systemTopSide: 128,
  systemFootCover: 82,
  /** A window on its side at most this high is the cover screen (466); the inner screen on its side is 669. */
  coverSideHeight: 500,
  /** Above the first button where the bar's top is free (the cover screen on its side). */
  freeTop: 8,
  /** Between the page's group and the places, at least. */
  groupGap: 12,
  /** Below the last button, over the bar's foot (the safe area's bottom adds to it). */
  foot: 8,
} as const;

export type RailEdge = "left" | "right";

/**
 * The edge of the system's vertical bar, or null where there is none: iOS, an iPhone, a side inset of the bar's
 * width on one side and nothing on the other (the left app in Split View has it on its left). Never Android, the
 * web or a desktop, and never a phone's cutout alone.
 */
export function railEdge(env: { os: string; phone: boolean; safe: { left: number; right: number } }): RailEdge | null {
  if (env.os !== "ios" || !env.phone) return null;
  const { left, right } = env.safe;
  if (right >= RAIL.minBar && left < 1) return "right";
  if (left >= RAIL.minBar && right < 1) return "left";
  return null;
}

/** The app's fixed places, top to bottom at the rail's foot: the picks are this table (the owner may change them). */
export const RAIL_PLACES = [
  { id: "brain", label: "Brain" },
  { id: "state", label: "Running and needs you" },
  { id: "search", label: "Search or run" },
  { id: "screen", label: "Screen" },
  { id: "new", label: "New session" },
] as const;

export type RailPlaceId = (typeof RAIL_PLACES)[number]["id"];

/** Where the rail's buttons may start (`top`) and must end (`bottom`, from the window's foot), in points. */
export type RailEnds = { top: number; bottom: number };

/**
 * The rail's ends: past the system's items at the camera's end of the bar (`RAIL.systemTop*`, `systemFootCover`),
 * the safe area, and the keyboard while it is up (the rail's foot rises above it, and what no longer fits goes into
 * More). Only the bar on the right was measured (every posture's own rotation); a bar on the left (the other
 * rotation, or the left app in Split View) keeps the system's reserve clear at both ends, since its camera's end is
 * not known.
 */
export function railEnds(window: { width: number; height: number }, safe: { top: number; bottom: number }, edge: RailEdge | null, keyboard = 0): RailEnds {
  const side = window.width > window.height;
  const cover = side && window.height <= RAIL.coverSideHeight;
  const reserve = cover ? RAIL.systemFootCover + RAIL.freeTop : side ? RAIL.systemTopSide : RAIL.systemTopUpright;
  const measured = edge === "right";
  const top = Math.max(safe.top, measured && cover ? RAIL.freeTop : reserve);
  const foot = Math.max(safe.bottom, cover ? RAIL.systemFootCover : measured ? 0 : reserve, keyboard);
  return { top, bottom: foot + RAIL.foot };
}

/** The bar's height left for Mewla's buttons, in points. */
export function railRoom(height: number, ends: RailEnds): number {
  return Math.max(0, height - ends.top - ends.bottom);
}

/**
 * A page's controls in the rail's order: Back first, then the controls that stay while the page is shown, then those
 * that come and go during the stay (`transient`: Stop while a turn runs, a file's Refresh once it changed). A control
 * that appears then never moves another under a finger: on the Duo simulator a Stop appearing above Brain's ⋯ took a
 * tap meant for the menu and stopped a live turn (round 4, 11-19).
 */
export function railPageOrder<T extends { transient?: boolean }>(back: readonly T[], actions: readonly T[]): T[] {
  return [...back, ...actions.filter((action) => !action.transient), ...actions.filter((action) => action.transient)];
}

export type RailLayout<T> = {
  /** The page's controls, from the top of the rail's room down. */
  top: T[];
  /** The places, at the foot. */
  bottom: T[];
  /** What did not fit, in order, for the "More" menu at the foot (empty: no More). */
  more: T[];
};

/**
 * Lays the page's controls (top) and the places (bottom) into `room` points. All of them show when they fit with the
 * gap between the groups; otherwise the last button at the foot is More, and the items from the end of the order (the
 * last place first, then the page's last control) go into it until the rest fits.
 */
export function railLayout<T>(page: readonly T[], places: readonly T[], room: number): RailLayout<T> {
  /** The height of `top` controls over `bottom` buttons at the foot. */
  const needs = (top: number, bottom: number) => (top + bottom) * RAIL.target + (top > 0 && bottom > 0 ? RAIL.groupGap : 0);
  if (needs(page.length, places.length) <= room) return { top: [...page], bottom: [...places], more: [] };
  const order = [...page, ...places];
  let shown = order.length - 1;
  // More is a button at the foot of its own.
  while (shown > 0 && needs(Math.min(shown, page.length), shown - Math.min(shown, page.length) + 1) > room) shown--;
  const top = order.slice(0, Math.min(shown, page.length));
  return { top, bottom: order.slice(top.length, shown), more: order.slice(shown) };
}
