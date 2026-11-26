/**
 * The Work board's layout (ui/work/WorkBoard.tsx): its groups (Needs you, Running, Today, Done) as side-by-side
 * columns over the whole pane. The pane is cut into card tracks (300 or wider); each column gets one, and the rest go,
 * one at a time, to the column that runs longest, while one more shortens it. So 13 Running Sessions beside 3 for
 * Today take four tracks to Today's one, and the columns end near one line; tracks nothing needs widen every card.
 * With fewer tracks than groups a column holds more than one, each placed under the column with the fewest cards.
 */
export const BOARD_PADDING = 24;
/** Between columns and between cards. */
export const BOARD_GAP = 16;
const TRACK_MIN = 300;

export type BoardColumn = {
  /** The groups it holds, by their index in the input, top to bottom. */
  groups: number[];
  /** Cards a row. */
  tracks: number;
  width: number;
};
export type BoardLayout = { columns: BoardColumn[]; cardWidth: number };

export function boardLayout(width: number, counts: readonly number[]): BoardLayout {
  const groups = counts.map((count, index) => ({ index, count })).filter((g) => g.count > 0);
  const inner = Math.max(0, width - 2 * BOARD_PADDING);
  const tracks = Math.max(1, Math.floor((inner + BOARD_GAP) / (TRACK_MIN + BOARD_GAP)));
  const n = Math.max(1, Math.min(tracks, groups.length));
  const held: number[][] = Array.from({ length: n }, () => []);
  const load = new Array<number>(n).fill(0);
  groups.forEach((group, order) => {
    const at = order < n ? order : load.indexOf(Math.min(...load));
    held[at]!.push(group.index);
    load[at]! += group.count + 1;
  });
  // How many rows a column runs to at t cards a row: each group's cards, wrapped, and its label.
  const rows = (at: number, t: number) => held[at]!.reduce((sum, index) => sum + Math.ceil(counts[index]! / t) + 1, 0);
  const given = new Array<number>(n).fill(1);
  for (let left = tracks - n; left > 0; left--) {
    let longest = 0;
    for (let at = 1; at < n; at++) if (rows(at, given[at]!) > rows(longest, given[longest]!)) longest = at;
    if (rows(longest, given[longest]! + 1) >= rows(longest, given[longest]!)) break;
    given[longest]!++;
  }
  const used = given.reduce((sum, t) => sum + t, 0);
  const cardWidth = Math.floor((inner - (used - 1) * BOARD_GAP) / used);
  return {
    columns: held.map((list, at) => ({ groups: list, tracks: given[at]!, width: given[at]! * cardWidth + (given[at]! - 1) * BOARD_GAP })),
    cardWidth,
  };
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
