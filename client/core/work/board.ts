/**
 * The Work board's layout (ui/work/WorkBoard.tsx): its groups side by side when each column would be 300 to 560
 * wide, else one under another with the cards in a grid of 320 or wider. `width` is the board's own width.
 */
export const BOARD_PADDING = 24;
export const BOARD_GAP = 12;
const COLUMN_MIN = 300;
const COLUMN_MAX = 560;
const CARD_MIN = 320;

export function boardColumns(width: number, groups: number): { side: boolean; card: number } {
  const inner = Math.max(0, width - 2 * BOARD_PADDING);
  const column = groups > 1 ? (inner - (groups - 1) * 16) / groups : 0;
  if (groups > 1 && column >= COLUMN_MIN && column <= COLUMN_MAX) return { side: true, card: Math.floor(column) };
  const perRow = Math.max(1, Math.floor((inner + BOARD_GAP) / (CARD_MIN + BOARD_GAP)));
  return { side: false, card: Math.floor((inner - (perRow - 1) * BOARD_GAP) / perRow) };
}
