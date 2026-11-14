import type { Frame, Transport } from "../transport";
import { rowText } from "./text";

/** Where a viewer stands in a session host Session's numbered scrollback. */
export type HistoryCursor = { epoch: number; next: number };

/** One pane of the attached view, in its grid's cells; `alternate` panes have no history to scroll. */
export type PaneScrollMode = {
  id: string;
  left: number;
  top: number;
  cols: number;
  rows: number;
  alternate: boolean;
  mouse: boolean;
  alt_scroll?: boolean;
  app_cursor?: boolean;
  /** How the program wants mouse reports encoded ("sgr", "urxvt", "utf8", "x10"); absent from older computers. */
  mouse_format?: string;
};

/** A tmux Session's answer: one bounded snapshot of the active pane's history, newest last. */
export type HistorySnapshot = {
  pane_id: string;
  cols: number;
  total: number;
  alternate: boolean;
  ansi: string;
  panes?: PaneScrollMode[];
};

/**
 * A session host Session's answer to a cursor: the rows after it. `reset`
 * voids what the viewer holds. Rows are numbered from the first the daemon saw
 * of this epoch; `first` is the oldest still kept, `from` the number of
 * lines[0], `next` the one after the last. `flags` has HISTORY_ROW_* bits.
 */
export type HistoryDelta = {
  pane_id: string;
  cols: number;
  rows: number;
  alternate: boolean;
  mouse: boolean;
  alt_scroll?: boolean;
  app_cursor?: boolean;
  mouse_format?: string;
  epoch: number;
  reset: boolean;
  first: number;
  from: number;
  next: number;
  lines: string[];
  flags?: number[];
};

/** Bits of a history row's flags. */
export const HISTORY_ROW_WRAPPED = 1;
export const HISTORY_ROW_CONTINUATION = 2;
export const HISTORY_ROW_PROMPT = 4;
export const HISTORY_ROW_PROMPT_MORE = 8;

export const HISTORY_MAX_ROWS = 6000;
export const HISTORY_MAX_BYTES = 4 * 1024 * 1024;

/**
 * What the view does with the history it holds: void it (`reset`), drop
 * `removed` rows from its front, then append `rows` (styled, ANSI) drawn at
 * `cols`. `alternate`: the program is on the alternate screen, so a drag is
 * the program's, not the history's.
 */
export type HistoryRender = {
  reset: boolean;
  removed: number;
  rows: string[];
  cols: number;
  alternate: boolean;
};

export function isHistoryDelta(history: unknown): history is HistoryDelta {
  return Array.isArray((history as HistoryDelta | null)?.lines);
}

const isCount = (value: unknown, max = Number.MAX_SAFE_INTEGER): value is number =>
  typeof value === "number" && Number.isInteger(value) && value >= 0 && value <= max;

/** The snapshot's rows, after checking its shape and bounds; none on the alternate screen. */
export function snapshotRows(snapshot: HistorySnapshot): string[] {
  if (!snapshot || typeof snapshot.pane_id !== "string" || !snapshot.pane_id || snapshot.pane_id.length > 128 ||
      typeof snapshot.alternate !== "boolean" || typeof snapshot.ansi !== "string" ||
      snapshot.ansi.length > HISTORY_MAX_BYTES || !isCount(snapshot.cols, 1000) || snapshot.cols < 1 ||
      !isCount(snapshot.total)) {
    throw new Error("Invalid terminal history snapshot.");
  }
  if (snapshot.alternate || !snapshot.ansi) return [];
  const rows = snapshot.ansi.replace(/\n$/, "").split("\n");
  if (rows.length > HISTORY_MAX_ROWS) throw new Error("Terminal history is too large.");
  return standAloneRows(rows);
}

const SGR = /\x1b\[([0-9;:]*)m/g;

/** The SGR sequences in force after `row`, given those in force before it (`pen`, "" for none). */
function penAfter(pen: string, row: string): string {
  for (const match of row.matchAll(SGR)) {
    const tokens = match[1] === "" ? ["0"] : match[1].split(";");
    const kept: string[] = [];
    for (let i = 0; i < tokens.length; i++) {
      const main = tokens[i].split(":")[0];
      if (main === "" || main === "0") {
        // A reset: what came before it, in this sequence or the rows above, is gone.
        pen = "";
        kept.length = 0;
      } else if ((main === "38" || main === "48" || main === "58") && !tokens[i].includes(":")) {
        // An extended color with ;-separated parts: its parts (the 0 in 38;5;0 is a color, not a reset).
        const parts = tokens[i + 1] === "5" ? 3 : tokens[i + 1] === "2" ? 5 : 1;
        kept.push(tokens.slice(i, i + parts).join(";"));
        i += parts - 1;
      } else {
        kept.push(tokens[i]);
      }
    }
    if (kept.length) pen += "\x1b[" + kept.join(";") + "m";
  }
  return pen;
}

/**
 * Rows that each start from what is in force where they begin. tmux captures a style once and the
 * rows after it lean on it; a view draws any page of history rows on its own (lazily, as they come
 * into sight), so a row that leans on the rows above it starts with a reset and that style. A
 * session host's rows stand alone already (daemon/ghosttyvt, Styled).
 */
export function standAloneRows(rows: readonly string[]): string[] {
  let pen = "";
  return rows.map((row) => {
    const out = pen ? "\x1b[0m" + pen + row : row;
    pen = penAfter(pen, row);
    return out;
  });
}

/** Checks a delta's shape and bounds before the view allocates for it. */
export function validateHistoryDelta(delta: HistoryDelta): HistoryDelta {
  const bad = () => new Error("Invalid terminal history delta.");
  if (!delta || typeof delta.pane_id !== "string" || !delta.pane_id || delta.pane_id.length > 128 ||
      typeof delta.alternate !== "boolean" || typeof delta.mouse !== "boolean" ||
      typeof delta.reset !== "boolean" || !Array.isArray(delta.lines) ||
      !isCount(delta.cols, 1000) || delta.cols < 1 || !isCount(delta.rows, 1000) ||
      !isCount(delta.epoch) || !isCount(delta.first) || !isCount(delta.from) || !isCount(delta.next) ||
      delta.first > delta.from || delta.from + delta.lines.length !== delta.next ||
      delta.lines.length > HISTORY_MAX_ROWS ||
      (delta.flags !== undefined && (!Array.isArray(delta.flags) || delta.flags.length !== delta.lines.length ||
        delta.flags.some((flag) => !isCount(flag, 15))))) throw bad();
  let bytes = 0;
  for (const line of delta.lines) {
    if (typeof line !== "string") throw bad();
    bytes += line.length;
  }
  if (bytes > HISTORY_MAX_BYTES) throw new Error("Terminal history is too large.");
  return delta;
}

/**
 * Longest suffix of `previous` that is a prefix of `next`, in linear time
 * (KMP): what a new tmux snapshot keeps of the one held, so only the rows
 * after it are new and the rows before it scrolled out of the capture.
 */
export function retainedOverlap(previous: readonly string[], next: readonly string[]): number {
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

/**
 * What the App holds of a Session's scrollback and how it brings it up to
 * date (Expo: terminalHistoryModel.ts). A session host Session sends only the
 * rows it is missing; a tmux Session sends snapshots, and the overlap with
 * what is held tells what is new. Both end as the same instruction for the
 * view: void, drop from the front, append. It also keeps each row's plain
 * text, which search and the prompt jumps read.
 */
export class TerminalHistoryModel {
  paneId = "";
  cols = 0;
  /** Number of lines[0] in the host's numbering; unused for snapshots. */
  first = 0;
  epoch = 0;
  next = 0;
  /** The host numbers rows and answers with deltas. */
  numbered = false;
  alternate = false;
  lines: string[] = [];
  text: string[] = [];
  flags: number[] = [];
  panes: PaneScrollMode[] = [];

  cursor(): HistoryCursor {
    return { epoch: this.numbered ? this.epoch : 0, next: this.numbered ? this.next : 0 };
  }

  clear(): void {
    this.paneId = "";
    this.cols = 0;
    this.first = 0;
    this.epoch = 0;
    this.next = 0;
    this.numbered = false;
    this.alternate = false;
    this.lines = [];
    this.text = [];
    this.flags = [];
    this.panes = [];
  }

  /** The instruction that builds this history on a view holding none. */
  replay(): HistoryRender {
    return { reset: true, removed: 0, rows: this.lines.slice(), cols: this.cols, alternate: this.alternate };
  }

  /** Whether the delta follows what is held, as numbered. */
  continues(delta: HistoryDelta): boolean {
    return this.numbered && !delta.reset && delta.epoch === this.epoch && delta.from === this.next &&
      delta.first >= this.first && delta.first - this.first <= this.lines.length &&
      this.first + this.lines.length === this.next;
  }

  /**
   * Whether the delta is a host's answer while it has numbered no rows yet: a program that started on
   * the alternate screen (Claude Code) leaves nothing to number, and the host answers a view
   * that holds nothing with nothing at epoch 0. That is in step. Asking again would void the view
   * every few hundred milliseconds, and its selection with it.
   */
  private startsEmpty(delta: HistoryDelta): boolean {
    return !this.numbered && !this.lines.length && delta.epoch === 0 && delta.first === 0 && delta.from === 0 &&
      delta.next === 0 && !delta.lines.length;
  }

  /** The view's instruction for a delta, null when nothing it shows changed, or "resync" when the delta does not follow what is held. */
  applyDelta(delta: HistoryDelta): HistoryRender | null | "resync" {
    if (!delta.reset && !this.continues(delta) && !this.startsEmpty(delta)) return "resync";
    const flags = delta.flags && delta.flags.length === delta.lines.length ? delta.flags : delta.lines.map(() => 0);
    const modesChanged = delta.alternate !== this.alternate || delta.cols !== this.cols || delta.pane_id !== this.paneId;
    let render: HistoryRender;
    if (delta.reset) {
      this.lines = delta.lines.slice();
      this.text = delta.lines.map(rowText);
      this.flags = flags.slice();
      render = { reset: true, removed: 0, rows: delta.lines.slice(), cols: delta.cols, alternate: delta.alternate };
    } else {
      const removed = delta.first - this.first;
      this.lines = this.lines.slice(removed).concat(delta.lines);
      this.text = this.text.slice(removed).concat(delta.lines.map(rowText));
      this.flags = this.flags.slice(removed).concat(flags);
      render = { reset: false, removed, rows: delta.lines.slice(), cols: delta.cols, alternate: delta.alternate };
    }
    const unchanged = !delta.reset && !delta.lines.length && !render.removed && !modesChanged && this.numbered;
    this.numbered = true;
    this.paneId = delta.pane_id;
    this.cols = delta.cols;
    this.epoch = delta.epoch;
    this.first = delta.first;
    this.next = delta.next;
    this.alternate = delta.alternate;
    this.panes = [{ id: delta.pane_id, left: 0, top: 0, cols: delta.cols, rows: delta.rows, alternate: delta.alternate, mouse: delta.mouse,
      ...(typeof delta.alt_scroll === "boolean" ? { alt_scroll: delta.alt_scroll } : {}),
      ...(typeof delta.app_cursor === "boolean" ? { app_cursor: delta.app_cursor } : {}),
      ...(typeof delta.mouse_format === "string" && delta.mouse_format.length <= 16 ? { mouse_format: delta.mouse_format } : {}) }];
    // A reset with what was held changes nothing the view shows only if both are empty.
    return unchanged ? null : render;
  }

  /** The view's instruction for a tmux snapshot, or null when it holds that snapshot already. */
  applySnapshot(snapshot: HistorySnapshot): HistoryRender | null {
    const rows = snapshotRows(snapshot);
    const panes = Array.isArray(snapshot.panes) ? snapshot.panes : [];
    const samePane = !this.numbered && this.paneId === snapshot.pane_id && this.cols === snapshot.cols;
    if (samePane && this.alternate === snapshot.alternate && rows.length === this.lines.length && rows.every((row, i) => row === this.lines[i])) {
      this.panes = panes;
      return null;
    }
    const overlap = samePane ? retainedOverlap(this.lines, rows) : 0;
    const reset = !samePane || (this.lines.length > 0 && overlap === 0);
    const render: HistoryRender = reset
      ? { reset: true, removed: 0, rows: rows.slice(), cols: snapshot.cols, alternate: snapshot.alternate }
      : { reset: false, removed: this.lines.length - overlap, rows: rows.slice(overlap), cols: snapshot.cols, alternate: snapshot.alternate };
    this.text = reset ? rows.map(rowText) : this.text.slice(render.removed).concat(render.rows.map(rowText));
    this.numbered = false;
    this.lines = rows;
    this.flags = rows.map(() => 0);
    this.paneId = snapshot.pane_id;
    this.cols = snapshot.cols;
    this.alternate = snapshot.alternate;
    this.panes = panes;
    return render;
  }
}

export interface HistoryHandlers {
  /** Rows for the view, in order. */
  rows(render: HistoryRender): void;
  /** A line about the history itself ("Loading history…", a failure); "" when there is none. */
  notice(text: string): void;
}

export const historyTiming = {
  /** A session host Session sends only the rows after the cursor, so a short wait after output is cheap. */
  deltaMs: 250,
  /** A tmux Session sends its whole snapshot. */
  snapshotMs: 750,
  timeoutMs: 10_000,
};

export const HISTORY_LOADING = "Loading history…";
export const HISTORY_FAILED = "History could not load. Tap to retry.";

/**
 * Keeps one attachment's history current (Expo: useTerminalHistory.ts):
 * `terminal_history` after the attachment opens and again a moment after
 * output, one request at a time. Every request names a cursor; a tmux
 * Session answers it with its snapshot. Answers for an attachment that has
 * since changed are dropped.
 */
export class TerminalHistoryFeed {
  readonly model = new TerminalHistoryModel();
  private session: string | null = null;
  private epoch = 0;
  private pending = false;
  private running = false;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private noticeShown = "";

  constructor(private readonly transport: Transport, private readonly handlers: HistoryHandlers) {}

  /** A new attachment (or none): what was held is void. */
  attach(session: string | null): void {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    this.epoch++;
    this.session = session;
    this.pending = Boolean(session);
    this.model.clear();
    this.handlers.rows({ reset: true, removed: 0, rows: [], cols: 0, alternate: false });
    this.notice(session ? HISTORY_LOADING : "");
    this.schedule();
  }

  /** Output arrived (or the grid changed): refresh soon. */
  output(): void {
    this.pending = true;
    this.schedule();
  }

  /** The person asked again after a failure. */
  retry(): void {
    if (!this.session) return;
    this.notice(HISTORY_LOADING);
    this.output();
  }

  get attached(): string | null {
    return this.session;
  }

  private notice(text: string): void {
    if (text === this.noticeShown) return;
    this.noticeShown = text;
    this.handlers.notice(text);
  }

  private schedule(): void {
    if (!this.session || this.running || this.timer || !this.pending) return;
    this.timer = setTimeout(() => void this.run(), this.model.numbered ? historyTiming.deltaMs : historyTiming.snapshotMs);
  }

  private async run(): Promise<void> {
    this.timer = null;
    const session = this.session;
    if (!session) return;
    const epoch = this.epoch;
    const current = () => this.session === session && this.epoch === epoch;
    this.running = true;
    this.pending = false;
    try {
      const frame = await this.transport.request<Frame>("terminal_history", { session_id: session, since: this.model.cursor() },
        { reply: "terminal_history", timeoutMs: historyTiming.timeoutMs });
      if (!current()) return;
      if (frame.session_id !== session || !frame.history) throw new Error("Invalid terminal history response.");
      if (isHistoryDelta(frame.history)) {
        const result = this.model.applyDelta(validateHistoryDelta(frame.history));
        if (result === "resync") {
          // It did not follow what is held: ask again from nothing.
          this.model.clear();
          this.handlers.rows({ reset: true, removed: 0, rows: [], cols: 0, alternate: false });
          this.pending = true;
          return;
        }
        if (result) this.handlers.rows(result);
      } else {
        const result = this.model.applySnapshot(frame.history as HistorySnapshot);
        if (result) this.handlers.rows(result);
      }
      this.notice("");
    } catch {
      if (current() && this.transport.state === "connected") this.notice(HISTORY_FAILED);
    } finally {
      this.running = false;
      if (!current() && this.session) this.pending = true;
      this.schedule();
    }
  }

  close(): void {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    this.epoch++;
    this.session = null;
    this.pending = false;
  }
}
