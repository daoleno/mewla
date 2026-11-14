/**
 * How a chat's rows reach its list, one narrow change at a time.
 *
 * NSH's List (0.5.0) diffs keyed rows by their common start and end only: everything between is one splice, laid
 * out again from estimated sizes, with the reader's place and any scroll the list was making lost. A full thread
 * slides that way on every event: the daemon keeps a conversation's newest 240 events, so a new one comes with the
 * oldest deleted, one at each end, and the list re-laid every row (owner 11-14: reading history snapped to the
 * newest; a send landed nowhere). Split in two, each step is a splice NSH keeps the screen still through: the
 * change after the rows the two lists share, then the change before them.
 */
import { keyedSplice } from "./scrollTrace";

type Row = { id: string };

/** Splits deeper than this go in one step (each level halves the change). */
const MAX_DEPTH = 4;

/**
 * The steps from `shown` (what the list holds) to `next`: arrays to give the list in order, the last one `next`.
 * One step when the change is already narrow (an append, a removal, one region), or when splitting would give the
 * list one key twice.
 */
export function rowSteps<T extends Row>(shown: readonly T[], next: readonly T[], depth = 0): (readonly T[])[] {
  const splice = keyedSplice(shown.map((row) => row.id), next.map((row) => row.id));
  if (!splice || splice.removed === 0 || splice.inserted === 0 || depth >= MAX_DEPTH) return [next];
  const run = longestSharedRun(shown, next, splice.at, splice.at + splice.removed, splice.at + splice.inserted);
  if (!run) return [next];
  // The list's own rows up to the shared run, then the new ones from it: only the part after the run changed.
  const middle = [...shown.slice(0, run.from), ...next.slice(run.to)];
  const keys = new Set<string>();
  for (const row of middle) {
    if (keys.has(row.id)) return [next];
    keys.add(row.id);
  }
  return [...rowSteps(shown, middle, depth + 1), ...rowSteps(middle, next, depth + 1)];
}

/**
 * The longest run of keys `shown[from, from + length)` that `next[to, to + length)` holds in the same order, inside
 * the changed regions (`shown` up to `shownEnd`, `next` up to `nextEnd`, both from `start`).
 */
function longestSharedRun(shown: readonly Row[], next: readonly Row[], start: number, shownEnd: number, nextEnd: number): { from: number; to: number; length: number } | null {
  const at = new Map<string, number>();
  for (let j = start; j < nextEnd; j += 1) at.set(next[j].id, j);
  let best: { from: number; to: number; length: number } | null = null;
  for (let i = start; i < shownEnd; ) {
    const j = at.get(shown[i].id);
    if (j === undefined) {
      i += 1;
      continue;
    }
    let length = 1;
    while (i + length < shownEnd && j + length < nextEnd && shown[i + length].id === next[j + length].id) length += 1;
    if (!best || length > best.length) best = { from: i, to: j, length };
    i += length;
  }
  return best;
}
