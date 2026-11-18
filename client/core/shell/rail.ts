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
   * The bar's top the system keeps: the Island and the status items, where a tap opens Control Center. On the Duo
   * simulator (iOS 27.2, 11-19) iOS reserves the inner screen's top 124 (an occlusion region; its bar layout guide
   * starts there), and the cover screen's status items end at 151 upright: the larger, with a margin, for both.
   * NSH reports neither the region nor the guide, so a Live Activity growing the Island down the bar is not
   * followed.
   */
  systemTop: 156,
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

/** The bar's height left for Mewla's buttons, in points: the window's height less the system's items at its top and the foot. */
export function railRoom(height: number, safe: { top: number; bottom: number }): number {
  return Math.max(0, height - Math.max(RAIL.systemTop, safe.top) - safe.bottom - RAIL.foot);
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
