import type { TerminalHistorySnapshot } from '../../services/terminalHistory';

export const TERMINAL_HISTORY_MAX_ROWS = 6000;
export const TERMINAL_HISTORY_MAX_BYTES = 4 * 1024 * 1024;
export const TERMINAL_HISTORY_MAX_HTML = 16 * 1024 * 1024;

export function historyRows(snapshot: TerminalHistorySnapshot): string[] {
  if (!snapshot || typeof snapshot.pane_id !== 'string' ||
      !/^%[0-9]+$/.test(snapshot.pane_id) || typeof snapshot.alternate !== 'boolean' ||
      typeof snapshot.ansi !== 'string' ||
      snapshot.ansi.length > TERMINAL_HISTORY_MAX_BYTES ||
      !Number.isInteger(snapshot.cols) || snapshot.cols < 1 || snapshot.cols > 1000 ||
      !Number.isInteger(snapshot.total) || snapshot.total < 0) {
    throw new Error('Invalid terminal history snapshot.');
  }
  if (snapshot.alternate || !snapshot.ansi) return [];
  const rows = snapshot.ansi.replace(/\n$/, '').split('\n');
  if (rows.length > TERMINAL_HISTORY_MAX_ROWS) throw new Error('Terminal history is too large.');
  return rows;
}

/** Longest old suffix that remains the new prefix, in linear time. Captures
 * remain immutable during gestures and selection. Once idle, this overlap
 * preserves the viewport row as the bounded tail discards old rows.
 */
export function retainedHistoryOverlap(previous: readonly string[], next: readonly string[]): number {
  if (!previous.length || !next.length) return 0;
  const prefix = new Array<number>(next.length).fill(0);
  for (let i = 1, j = 0; i < next.length; i++) {
    while (j && next[i] !== next[j]) j = prefix[j - 1];
    if (next[i] === next[j]) j++;
    prefix[i] = j;
  }
  let matched = 0;
  for (const row of previous) {
    while (matched && (matched === next.length || row !== next[matched])) matched = prefix[matched - 1];
    if (row === next[matched]) matched++;
  }
  return matched;
}

export interface TerminalHistoryRender {
  rows: string[];
  removed: number;
  reset: boolean;
  notice: string;
}
