import type { HistoryRender } from "./history";
import { NO_SEARCH, promptRows, promptTarget, searchHistory, type SearchState } from "./text";

/** What the navigator moves: the terminal view's history viewport. */
export interface TerminalViewport {
  /** Show history row `row` at the top. */
  scrollTo(row: number): void;
  /** Back to the live screen. */
  scrollToBottom(): void;
  /** Highlight one history row (the search's current match); -1 none. */
  mark(row: number): void;
}

/** The rows the view holds: their plain text and flags (TerminalHistoryModel). */
export type HistoryRows = { text: readonly string[]; flags: readonly number[] };

/**
 * Moving through the history while scrolled up (Expo: the WebView's
 * __mewlaJumpPrompt / __mewlaSearch): the previous and next prompt from the
 * row at the top, and a search over the rows' text that steps through its
 * matches. Both run on the phone, over the history the view holds.
 */
export class TerminalNavigator {
  /** The history row at the top of the view; the history's length at the bottom. */
  private top = 0;
  private atBottom = true;
  private gridRows = 24;
  search: SearchState = NO_SEARCH;

  constructor(private readonly rows: () => HistoryRows, private readonly view: TerminalViewport, private readonly changed: (search: SearchState) => void = () => {}) {}

  /** The view's grid height, for where a match lands. */
  setGridRows(rows: number): void {
    if (rows > 0) this.gridRows = rows;
  }

  /** The view reported where it stands. */
  viewport(top: number, atBottom: boolean): void {
    this.top = Math.max(0, top);
    this.atBottom = atBottom;
  }

  get scrolledUp(): boolean {
    return !this.atBottom;
  }

  /** -1 the previous prompt above the top row, 1 the next one; past the last, the live screen. */
  jumpPrompt(direction: -1 | 1): void {
    const { text, flags } = this.rows();
    const top = this.atBottom ? text.length : this.top;
    const target = promptTarget(promptRows(text, flags), top, direction);
    if (target === "bottom") this.view.scrollToBottom();
    else if (target !== null) this.view.scrollTo(target);
  }

  /** `step` 0 a new query (the newest match), -1 an older match, 1 a newer one. */
  runSearch(query: string, step: -1 | 0 | 1): SearchState {
    const next = searchHistory(this.rows().text, query, step, this.search);
    this.setSearch(next);
    if (next.current >= 0) this.view.scrollTo(Math.max(0, next.current - Math.floor(this.gridRows / 3)));
    return next;
  }

  /** The same query again, after a step: older, or newer. */
  step(step: -1 | 1): SearchState {
    return this.search.query ? this.runSearch(this.search.query, step) : this.search;
  }

  clearSearch(): void {
    this.setSearch(NO_SEARCH);
  }

  /** The view's rows changed: the matches follow them without moving the view. */
  historyChanged(render: HistoryRender): void {
    if (!this.search.query) return;
    const previous = render.reset ? NO_SEARCH : { ...this.search, current: this.search.current >= 0 ? this.search.current - render.removed : -1 };
    this.setSearch(searchHistory(this.rows().text, this.search.query, null, previous));
  }

  private setSearch(next: SearchState): void {
    const moved = next.current !== this.search.current;
    this.search = next;
    if (moved) this.view.mark(next.current);
    this.changed(next);
  }
}
