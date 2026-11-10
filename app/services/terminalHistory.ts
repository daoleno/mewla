/** Wire response for the authenticated, attachment-scoped terminal_history API
 * of a tmux Session: one bounded snapshot of the pane's history. */
export interface TerminalHistorySnapshot {
  pane_id: string;
  cols: number;
  total: number;
  alternate: boolean;
  ansi: string;
  panes?: TerminalPaneScrollMode[];
}

/** Where a viewer stands in a session host Session's numbered scrollback. */
export interface TerminalHistoryCursor {
  epoch: number;
  next: number;
}

/** Wire response for a request that names a cursor: the rows after it. A
 * `reset` voids what the viewer holds and `lines` replace it. Rows are
 * numbered from the first the daemon saw of this epoch; `first` is the oldest
 * still kept, `from` the number of lines[0], `next` the one after the last.
 * `flags` carries one TERMINAL_ROW_* bit set per line when the host marks
 * them, and the modes are left out when the host does not know them. */
export interface TerminalHistoryDelta {
  pane_id: string;
  cols: number;
  rows: number;
  alternate: boolean;
  mouse: boolean;
  alt_scroll?: boolean;
  app_cursor?: boolean;
  epoch: number;
  reset: boolean;
  first: number;
  from: number;
  next: number;
  lines: string[];
  flags?: number[];
}

export type TerminalHistoryResponse = TerminalHistorySnapshot | TerminalHistoryDelta;

export function isTerminalHistoryDelta(
  response: TerminalHistoryResponse,
): response is TerminalHistoryDelta {
  return Array.isArray((response as TerminalHistoryDelta)?.lines);
}

export interface TerminalPaneScrollMode {
  id: string;
  left: number;
  top: number;
  cols: number;
  rows: number;
  alternate: boolean;
  mouse: boolean;
  /** The wheel sends cursor keys on the alternate screen (mode 1007). Absent
   * when the host does not say, which is not the same as off. */
  alt_scroll?: boolean;
  /** Cursor keys are the application's (DECCKM). */
  app_cursor?: boolean;
}

/** Bits of a history row's flags. */
export const TERMINAL_ROW_WRAPPED = 1;
export const TERMINAL_ROW_CONTINUATION = 2;
export const TERMINAL_ROW_PROMPT = 4;
export const TERMINAL_ROW_PROMPT_MORE = 8;
