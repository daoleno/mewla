import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { mixHex } from "../../theme/colorUtils";

export type MessageTableRowTone = "section" | "even" | "odd";

export function messageTableRowTone(
  row: string[],
  rowIndex: number,
): MessageTableRowTone {
  const populatedCells = row.filter((cell) => cell.trim().length > 0);
  if (populatedCells.length === 1) {
    return "section";
  }
  return rowIndex % 2 === 0 ? "even" : "odd";
}

export function messageTableSemanticColors(chrome: TerminalThemeChrome) {
  return {
    // A touch of ink on the quiet fill, so the header outranks zebra rows.
    header: mixHex(chrome.surfaceMuted, chrome.text, 0.16),
    section: chrome.accentSoft,
    even: chrome.surface,
    odd: chrome.disabledSurface,
  } as const;
}

/** Cell width limits; `compact` is the narrow chat layout. */
export const MESSAGE_TABLE_CELL_LIMITS = {
  regular: { min: 72, max: 240, padding: 10 },
  compact: { min: 64, max: 200, padding: 8 },
} as const;

/** Advances of 13 pt Inter glyphs by class; digits and capitals run wider than lowercase. */
const THIN_ADVANCE = 4;
const NARROW_ADVANCE = 7.2;
const BROAD_ADVANCE = 8.6;
const WIDE_ADVANCE = 13.5;
const THIN_CHAR = /[\s.,:;'"|!()[\]{}ilj]/;
const BROAD_CHAR = /[0-9A-Zmw%$#@&]/;
const WIDE_CHAR = /[ᄀ-ᅟ⺀-꓏가-힣豈-﫿︰-﹏＀-｠￠-￦]|[\u{1f300}-\u{1faff}]/u;

/** What a cell shows, without the markdown that draws it. */
function visibleCellText(cell: string): string {
  return cell
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/(\*\*|__|\*|_|~~|`)/g, "");
}

function estimatedTextWidth(text: string): number {
  let width = 0;
  for (const char of text) {
    if (WIDE_CHAR.test(char)) width += WIDE_ADVANCE;
    else if (BROAD_CHAR.test(char)) width += BROAD_ADVANCE;
    else if (THIN_CHAR.test(char)) width += THIN_ADVANCE;
    else width += NARROW_ADVANCE;
  }
  return width;
}

/**
 * One width per column, from its widest cell, so every row's cells line up.
 * Rows lay out independently, so without a shared width each row sized its
 * cells to its own text and the column edges zigzagged. Text past the
 * maximum wraps inside the cell.
 */
export function messageTableColumnWidths(
  headers: readonly string[],
  rows: readonly (readonly string[])[],
  compact = false,
): number[] {
  const limits = compact ? MESSAGE_TABLE_CELL_LIMITS.compact : MESSAGE_TABLE_CELL_LIMITS.regular;
  const columns = Math.max(headers.length, ...rows.map((row) => row.length));
  return Array.from({ length: columns }, (_, column) => {
    const cells = [headers[column] ?? "", ...rows.map((row) => row[column] ?? "")];
    const widest = Math.max(...cells.map((cell) => estimatedTextWidth(visibleCellText(cell))));
    const width = Math.ceil(widest + limits.padding * 2 + 2);
    return Math.min(limits.max, Math.max(limits.min, width));
  });
}
