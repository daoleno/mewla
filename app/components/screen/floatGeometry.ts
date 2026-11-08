/** Where the floating screen sits and how big it is: the same for every kind of agent screen. */
export type FloatCorner = "top-left" | "top-right" | "bottom-left" | "bottom-right";

/** Bottom-right, above the composer: the chat's text is read from the left, so most of it stays in view. */
export const DEFAULT_FLOAT_CORNER: FloatCorner = "bottom-right";

export function parseFloatCorner(value: string | null): FloatCorner {
  return value === "top-left" || value === "top-right" || value === "bottom-left" || value === "bottom-right"
    ? value
    : DEFAULT_FLOAT_CORNER;
}

/** The free rectangle the float may sit in: above the composer and the keyboard, under the header. */
export interface FloatArea {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/** Where the float's top-left corner goes for a corner of the free area. */
export function floatOrigin(corner: FloatCorner, area: FloatArea, size: { width: number; height: number }, margin: number) {
  const x = corner.endsWith("left") ? area.left + margin : area.right - margin - size.width;
  const y = corner.startsWith("top") ? area.top + margin : area.bottom - margin - size.height;
  // A tiny area keeps the float on screen rather than off its top or left.
  return { x: Math.max(area.left, x), y: Math.max(area.top, y) };
}

/** The corner nearest the float's centre once the finger lets go. */
export function nearestFloatCorner(center: { x: number; y: number }, area: FloatArea): FloatCorner {
  const horizontal = center.x < (area.left + area.right) / 2 ? "left" : "right";
  const vertical = center.y < (area.top + area.bottom) / 2 ? "top" : "bottom";
  return `${vertical}-${horizontal}`;
}

/** The float's width as a share of the window: where it starts, what a double tap enlarges it to, and how far the person may take it. */
export const FLOAT_DEFAULT = 0.55;
export const FLOAT_LARGE = 0.8;
export const FLOAT_MIN = 0.3;
export const FLOAT_MAX = 0.9;
/** On a wide window the default and the large size stay corner windows, not half the page; a size the person drags to is theirs. */
export const FLOAT_WIDE = 420;
export const FLOAT_WIDE_LARGE = 840;

export function clampFloatFraction(value: number): number {
  return Math.min(FLOAT_MAX, Math.max(FLOAT_MIN, value));
}

/** The size the person left it at, or the default for anything that is not a share in range. */
export function parseFloatFraction(value: string | null): number {
  const parsed = value === null ? Number.NaN : Number(value);
  return Number.isFinite(parsed) && parsed >= FLOAT_MIN && parsed <= FLOAT_MAX ? parsed : FLOAT_DEFAULT;
}

/** A double tap goes between the default and the large size. */
export function toggledFloatFraction(fraction: number): number {
  return Math.abs(fraction - FLOAT_DEFAULT) < 0.01 ? FLOAT_LARGE : FLOAT_DEFAULT;
}

/**
 * The float's size: exactly the agent display's proportions, so its picture fills the window with
 * no bars, no stretching and no crop. The default stays under 60% of the window's height, a size
 * the person chose under 85%; a keyboard that takes room gives height first, down to the least width.
 */
export function floatSize(
  ratio: number,
  fraction: number,
  window: { width: number; height: number },
  freeHeight?: number,
) {
  const safe = ratio > 0 ? ratio : 9 / 20;
  const heightShare = Math.abs(fraction - FLOAT_DEFAULT) < 0.01 ? 0.6 : 0.85;
  let width = Math.min(fraction * window.width, heightShare * window.height * safe);
  if (Math.abs(fraction - FLOAT_DEFAULT) < 0.01) width = Math.min(width, FLOAT_WIDE);
  else if (Math.abs(fraction - FLOAT_LARGE) < 0.01) width = Math.min(width, FLOAT_WIDE_LARGE);
  if (freeHeight !== undefined) width = Math.min(width, freeHeight * safe);
  width = Math.max(width, FLOAT_MIN * window.width);
  return { width, height: width / safe };
}

/**
 * The width after a finger moved the corner opposite the float's own corner by (dx, dy): the
 * movement along the diagonal counts, so the proportions stay locked whichever way it is dragged.
 */
export function resizedFloatWidth(
  startWidth: number,
  ratio: number,
  delta: { x: number; y: number },
  corner: FloatCorner,
): number {
  const safe = ratio > 0 ? ratio : 9 / 20;
  const sx = corner.endsWith("left") ? 1 : -1;
  const sy = corner.startsWith("top") ? 1 : -1;
  return startWidth + (sx * delta.x + (sy * delta.y) / safe) / (1 + 1 / (safe * safe));
}
