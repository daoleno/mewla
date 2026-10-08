/**
 * Hand-drawn Mewla glyphs on Phosphor's 256 grid, stroked at its bold weight
 * (24) with round caps and joins so they sit beside the vendored set.
 */
export interface MewlaGlyph {
  /** Stroked at 24 with round caps and joins. */
  strokes: readonly string[];
  /** Filled solid, for dots. */
  fills?: readonly string[];
}

function dot(cx: number, cy: number, r: number): string {
  return `M${cx - r},${cy}a${r},${r} 0 1 0 ${r * 2},0a${r},${r} 0 1 0 ${-r * 2},0Z`;
}

export const MEWLA_GLYPHS = {
  /**
   * Brain: a round chat bubble with two soft cat ears, because the cat is
   * Brain. Ink only; the vermilion seal stays the one cat on a screen.
   */
  brain: {
    strokes: [
      "M100.6,64.8A80,80 0 0 1 155.4,64.8L188,44L193.5,94.1A80,80 0 0 1 71.4,196.6L44,224L52.8,167.4A80,80 0 0 1 62.5,94.1L68,44Z",
    ],
    fills: [dot(92, 142, 14), dot(128, 142, 14), dot(164, 142, 14)],
  },
  /** The two-line menu for the drawer control. */
  menu: {
    strokes: ["M48,100H208", "M48,156H208"],
  },
} as const satisfies Record<string, MewlaGlyph>;
