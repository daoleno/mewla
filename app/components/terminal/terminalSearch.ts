export interface TerminalSearchState {
  query: string;
  total: number;
  /** Position of the row shown among the matches, or -1. */
  index: number;
}

/** What the bar shows of the matches: where the shown row stands, or none. */
export function terminalSearchCount(state: TerminalSearchState): string {
  if (!state.query) return '';
  if (!state.total) return '0';
  return `${Math.max(0, state.index) + 1}/${state.total}`;
}
