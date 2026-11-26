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

/** What a request in the tray names, as the board reads it. */
export type BoardRequest = { workId?: string; workerId?: string; title: string };

/**
 * The request that asks something of a Work, if any: one naming the Work, or one of its Sessions (a Worker's
 * permission prompt). The tray lists it under Needs you, so the board puts the Work there too.
 */
export function workRequest(
  slip: { workId: string; sessionId?: string; workerSessions?: readonly { session_id: string }[] },
  requests: readonly BoardRequest[],
): BoardRequest | undefined {
  const sessions = new Set([slip.sessionId, ...(slip.workerSessions ?? []).map((w) => w.session_id)].filter(Boolean));
  return requests.find((r) => r.workId === slip.workId || (r.workerId !== undefined && sessions.has(r.workerId)));
}
