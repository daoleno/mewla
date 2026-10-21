/**
 * Where the anchored message menu goes. All values are in one coordinate
 * space (the overlay's own), in dp.
 */
export interface MenuRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface MenuBounds {
  top: number;
  bottom: number;
  left: number;
  right: number;
}

export interface MenuPlacementInput {
  /** The pressed message (or the button that opened the menu). */
  anchor: MenuRect;
  /** Where the finger is, for a message too tall to anchor to as a whole. */
  touchY: number;
  menu: { width: number; height: number };
  /** The visible area: the overlay clipped to the keyboard's top. */
  bounds: MenuBounds;
  /** Which of the anchor's edges the menu lines up with. */
  align: "start" | "end";
}

export type MenuSide = "below" | "above" | "touch";

export interface MenuPlacement {
  left: number;
  top: number;
  side: MenuSide;
  /** Where the menu grows from, in the menu's own box: toward the message. */
  origin: { x: number; y: number };
}

export const MENU_GAP = 8;
export const MENU_MARGIN = 12;

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), Math.max(min, max));
}

/**
 * Below the message when it fits, above it when that fits, and at the finger
 * otherwise: a message taller than the screen, or one half off it, has no
 * edge worth following, so the menu stays beside the part that is being held.
 */
export function placeMessageMenu({
  anchor,
  touchY,
  menu,
  bounds,
  align,
}: MenuPlacementInput): MenuPlacement {
  const minTop = bounds.top + MENU_MARGIN;
  const maxTop = bounds.bottom - MENU_MARGIN - menu.height;
  const anchorBottom = anchor.y + anchor.height;
  const visible = anchorBottom > bounds.top && anchor.y < bounds.bottom;
  const fitsBelow = anchorBottom + MENU_GAP <= maxTop;
  const fitsAbove = anchor.y - MENU_GAP - menu.height >= minTop;

  let side: MenuSide;
  let top: number;
  if (visible && fitsBelow) {
    side = "below";
    top = anchorBottom + MENU_GAP;
  } else if (visible && fitsAbove) {
    side = "above";
    top = anchor.y - MENU_GAP - menu.height;
  } else {
    side = "touch";
    const touch = clamp(touchY, bounds.top, bounds.bottom);
    top = touch + MENU_GAP <= maxTop ? touch + MENU_GAP : touch - MENU_GAP - menu.height;
    top = clamp(top, minTop, maxTop);
  }

  const edge = align === "end" ? anchor.x + anchor.width : anchor.x;
  const left = clamp(
    align === "end" ? edge - menu.width : edge,
    bounds.left + MENU_MARGIN,
    bounds.right - MENU_MARGIN - menu.width,
  );
  const originX = clamp(edge - left, 0, menu.width);
  return {
    left,
    top,
    side,
    origin: { x: originX, y: side === "above" ? menu.height : 0 },
  };
}
