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
    header: mixHex(chrome.surfaceMuted, chrome.text, 0.1),
    section: chrome.accentSoft,
    even: chrome.surface,
    odd: chrome.disabledSurface,
  } as const;
}
