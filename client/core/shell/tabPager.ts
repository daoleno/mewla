/**
 * Tabs that follow the finger (owner, 2026-11-13 20:05: 「有些做了 tab 逻辑的，但是无法响应手势滑动」). A page's tabs
 * (its segmented control) sit over a row of pages: a sideways swipe moves the pages with the finger and the
 * control's chosen segment with them, and a release settles like a pager. Taps on the segments still choose.
 *
 * The numbers and the arithmetic are here; `ui/kit/TabPager.tsx` draws it. Distances in points, velocities in
 * points per second (the pan's own), positive to the right.
 */
export const TAB_PAGER = {
  /** The pan takes the touch once it has moved this far sideways, and never once it moved this far up or down first (the page's vertical scroll keeps it). */
  activeOffsetX: 12,
  failOffsetY: 12,
  /** A flick carries the release on by velocity × this (seconds): a quick short swipe still turns the page. */
  fling: 0.15,
  /** The settle: a spring from wherever the finger let go, with its velocity. */
  spring: { stiffness: 400, damping: 40 },
} as const;

/**
 * The page a release settles on. `rest`: the page laid out under the row (the one the pan moves away from);
 * `from`: the page the swipe started on (the one going to, if it interrupted a settle); `offset`: how far the
 * row is moved from `rest`; `velocity` at release. Past half a page, or a flick, goes on, one page at most,
 * within the pages; anything less goes back.
 */
export function settlePage(options: { rest: number; from: number; count: number; width: number; offset: number; velocity: number }): number {
  const { rest, from, count, width, offset, velocity } = options;
  if (count <= 1 || width <= 0) return clamp(from, 0, Math.max(0, count - 1));
  const projected = Math.round(rest - (offset + velocity * TAB_PAGER.fling) / width);
  return clamp(clamp(projected, from - 1, from + 1), 0, count - 1);
}

/** Where the row is between pages, as a page index (1.5: halfway from the second to the third). The control's chosen segment follows it. */
export function pagerProgress(rest: number, offset: number, width: number): number {
  return width > 0 ? rest - offset / width : rest;
}

/**
 * The chosen segment's mark at each page, from the segments as laid out (`x` and `width` in the control): one
 * shape `base` wide at the control's left, moved by `translate` and stretched by `scale` (about its centre), so
 * segments of different widths (a control sized to its words, Calendar's) are each covered exactly at rest.
 */
export function indicatorStops(segments: ReadonlyArray<{ x: number; width: number }>, base: number): { translate: number[]; scale: number[] } {
  const b = base > 0 ? base : 1;
  return {
    translate: segments.map((s) => s.x + s.width / 2 - b / 2),
    scale: segments.map((s) => s.width / b),
  };
}

function clamp(n: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, n));
}
