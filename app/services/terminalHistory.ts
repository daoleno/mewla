/** Wire response for the authenticated, attachment-scoped terminal_history API. */
export interface TerminalHistorySnapshot {
  pane_id: string;
  cols: number;
  total: number;
  alternate: boolean;
  ansi: string;
}
