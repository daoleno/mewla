import type {
  TerminalHistoryCursor,
  TerminalHistoryDelta,
  TerminalPaneScrollMode,
} from '../../services/terminalHistory';
import {
  TERMINAL_HISTORY_MAX_BYTES,
  TERMINAL_HISTORY_MAX_ROWS,
  retainedHistoryOverlap,
  type TerminalHistoryRender,
} from './terminalHistory';

/** What the App holds of a Session's scrollback, and how it brings it up to
 * date: rows it was sent as ANSI (to draw again when the theme changes), the
 * rows as HTML, and where it stands in the host's numbering. A session host
 * Session sends only the rows it is missing; a tmux Session sends snapshots,
 * and the overlap with what is held tells what is new. Both end as the same
 * instruction to the renderer: void, drop from the front, append. */
export class TerminalHistoryModel {
  paneId = '';
  cols = 0;
  /** Number of lines[0] in the host's numbering; unused for snapshots. */
  first = 0;
  epoch = 0;
  next = 0;
  /** The host numbers rows and answers with deltas. */
  numbered = false;
  lines: string[] = [];
  rows: string[] = [];
  flags: number[] = [];
  panes: TerminalPaneScrollMode[] = [];

  cursor(): TerminalHistoryCursor {
    return { epoch: this.numbered ? this.epoch : 0, next: this.numbered ? this.next : 0 };
  }

  clear() {
    this.paneId = '';
    this.cols = 0;
    this.first = 0;
    this.epoch = 0;
    this.next = 0;
    this.numbered = false;
    this.lines = [];
    this.rows = [];
    this.flags = [];
    this.panes = [];
  }

  /** The instruction that builds this history on a renderer with none. */
  replay(notice = ''): TerminalHistoryRender {
    return {
      rows: this.rows.slice(), flags: this.flags.slice(), removed: 0, reset: true, notice,
      panes: this.panes, paneId: this.paneId,
    };
  }

  /** Whether the delta can be applied to what is held, as numbered. */
  continues(delta: TerminalHistoryDelta): boolean {
    return this.numbered && !delta.reset && delta.epoch === this.epoch &&
      delta.from === this.next && delta.first >= this.first &&
      delta.first - this.first <= this.rows.length &&
      this.first + this.rows.length === this.next;
  }

  /** Applies a delta, `html` being its lines drawn. Returns the instruction
   * for the renderer, null when nothing it knows has changed, or 'resync'
   * when the delta does not follow what is held and the whole history has to
   * be asked for again. */
  applyDelta(delta: TerminalHistoryDelta, html: string[]): TerminalHistoryRender | null | 'resync' {
    if (html.length !== delta.lines.length) return 'resync';
    if (!delta.reset && !this.continues(delta)) return 'resync';
    const panes = deltaPanes(delta);
    const panesChanged = JSON.stringify(panes) !== JSON.stringify(this.panes);
    const flags = delta.flags && delta.flags.length === html.length
      ? delta.flags : html.map(() => 0);
    let render: TerminalHistoryRender;
    if (delta.reset) {
      this.lines = delta.lines.slice();
      this.rows = html.slice();
      this.flags = flags.slice();
      render = { rows: html.slice(), flags: flags.slice(), removed: 0, reset: true, notice: '' };
    } else {
      const removed = delta.first - this.first;
      this.lines = this.lines.slice(removed).concat(delta.lines);
      this.rows = this.rows.slice(removed).concat(html);
      this.flags = this.flags.slice(removed).concat(flags);
      if (!html.length && !removed && !panesChanged) {
        this.next = delta.next;
        return null;
      }
      render = { rows: html.slice(), flags: flags.slice(), removed, reset: false, notice: '' };
    }
    this.numbered = true;
    this.paneId = delta.pane_id;
    this.cols = delta.cols;
    this.epoch = delta.epoch;
    this.first = delta.first;
    this.next = delta.next;
    this.panes = panes;
    render.panes = panes;
    render.paneId = delta.pane_id;
    return render;
  }

  /** Applies a tmux snapshot's rows, drawn as `html`. */
  applySnapshot(
    paneId: string,
    cols: number,
    ansi: string[],
    html: string[],
    panes: TerminalPaneScrollMode[],
  ): TerminalHistoryRender {
    const samePane = !this.numbered && this.paneId === paneId && this.cols === cols;
    const overlap = samePane ? retainedHistoryOverlap(this.rows, html) : 0;
    const render: TerminalHistoryRender = {
      rows: html.slice(overlap),
      removed: this.rows.length - overlap,
      reset: !samePane || (this.rows.length > 0 && overlap === 0),
      notice: '',
      panes,
      paneId,
    };
    if (render.reset) render.removed = 0;
    this.numbered = false;
    this.lines = ansi.slice();
    this.rows = html.slice();
    this.flags = [];
    this.paneId = paneId;
    this.cols = cols;
    this.panes = panes;
    return render;
  }
}

/** One pane covering the whole grid, as a host Session is. */
export function deltaPanes(delta: TerminalHistoryDelta): TerminalPaneScrollMode[] {
  const pane: TerminalPaneScrollMode = {
    id: delta.pane_id, left: 0, top: 0, cols: delta.cols, rows: delta.rows,
    alternate: delta.alternate, mouse: delta.mouse,
  };
  if (typeof delta.alt_scroll === 'boolean') pane.alt_scroll = delta.alt_scroll;
  if (typeof delta.app_cursor === 'boolean') pane.app_cursor = delta.app_cursor;
  return [pane];
}

const isCount = (value: unknown, max = Number.MAX_SAFE_INTEGER): value is number =>
  typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= max;

/** Checks a delta's shape and bounds before it reaches native allocation. */
export function validateHistoryDelta(delta: TerminalHistoryDelta): TerminalHistoryDelta {
  const bad = () => new Error('Invalid terminal history delta.');
  if (!delta || typeof delta.pane_id !== 'string' || !delta.pane_id || delta.pane_id.length > 128 ||
      typeof delta.alternate !== 'boolean' || typeof delta.mouse !== 'boolean' ||
      typeof delta.reset !== 'boolean' || !Array.isArray(delta.lines) ||
      !isCount(delta.cols, 1000) || delta.cols < 1 || !isCount(delta.rows, 1000) ||
      !isCount(delta.epoch) || !isCount(delta.first) || !isCount(delta.from) || !isCount(delta.next) ||
      delta.first > delta.from || delta.from + delta.lines.length !== delta.next ||
      delta.lines.length > TERMINAL_HISTORY_MAX_ROWS ||
      (delta.flags !== undefined && (!Array.isArray(delta.flags) || delta.flags.length !== delta.lines.length ||
        delta.flags.some((flag) => !isCount(flag, 15))))) throw bad();
  let bytes = 0;
  for (const line of delta.lines) {
    if (typeof line !== 'string') throw bad();
    bytes += line.length;
  }
  if (bytes > TERMINAL_HISTORY_MAX_BYTES) throw new Error('Terminal history is too large.');
  return delta;
}
