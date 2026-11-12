import type { Frame, Transport } from "../transport";
import { TerminalHistoryFeed, type HistoryRender, type TerminalHistoryModel } from "./history";

export type TerminalScrollState = { atBottom: boolean; inCopyMode: boolean; position: number };

export interface TerminalHandlers {
  /** A new attachment: clear the view before its output arrives. */
  reset(): void;
  output(data: string): void;
  /**
   * The Session's size, told before each replay (session host). When another viewer (mewla attach on a
   * computer) holds the size, it is not this view's grid: the view draws the screen at this size, fitted
   * into its area, until its person acts here and the size is the view's again. Older computers never say.
   */
  sessionSize(cols: number, rows: number): void;
  /** tmux copy mode on the computer (`terminal_scroll_state`); a session host Session never sends it. */
  scrollState(state: TerminalScrollState): void;
  /** The history above the screen changed: what the view does with the rows it holds. */
  history(render: HistoryRender): void;
  /** A line about the history ("Loading history…", a failure), "" for none. */
  historyNotice(text: string): void;
  /** The program ended; the view stays as it was and nothing reopens it. */
  exit(code: number | null): void;
  /** Opening failed, or the attachment broke; `message` is the computer's. */
  error(message: string): void;
}

/**
 * `terminal_opened` carries no request id: the daemon answers opens in order.
 * One open at a time per connection, so two terminals (two Session screens
 * on the stack) never take each other's attachment.
 */
const openQueues = new WeakMap<Transport, Promise<void>>();

const OPEN_TIMEOUT_MS = 15_000;

/**
 * One terminal attached to a Session's tmux pane over the current connection:
 * opens once the transport is connected and the view knows its size, opens
 * again after every reconnect (the daemon closes attachments with the socket),
 * and closes on `close()`. Never throws into the caller.
 */
export class TerminalSession {
  private sessionId: string | null = null;
  private size: { cols: number; rows: number } | null = null;
  private opening = false;
  /** The Session's size the computer told last (`terminal_size`). */
  private told: { cols: number; rows: number } | null = null;
  private closed = false;
  private exited = false;
  private readonly stops: Array<() => void> = [];
  private readonly feed: TerminalHistoryFeed;

  constructor(
    private readonly transport: Transport,
    private readonly targetId: string,
    private readonly handlers: TerminalHandlers,
  ) {
    this.feed = new TerminalHistoryFeed(transport, { rows: (render) => handlers.history(render), notice: (text) => handlers.historyNotice(text) });
    const mine = (frame: Frame) => this.sessionId !== null && frame.session_id === this.sessionId;
    this.stops.push(
      transport.on("connected", () => this.open()),
      transport.on("disconnected", () => {
        this.sessionId = null;
        this.opening = false;
        if (!this.exited && !this.closed) this.feed.attach(null);
      }),
      transport.on("terminal_output", (frame) => {
        if (!mine(frame) || typeof frame.data !== "string") return;
        handlers.output(frame.data);
        this.feed.output();
      }),
      transport.on("terminal_size", (frame) => {
        const cols = Number(frame.cols), rows = Number(frame.rows);
        if (!mine(frame) || !(cols >= 2 && rows >= 2)) return;
        handlers.sessionSize(cols, rows);
        // The Session was resized: its history comes again at the new width.
        if (this.told && (this.told.cols !== cols || this.told.rows !== rows)) this.feed.output();
        this.told = { cols, rows };
      }),
      transport.on("terminal_scroll_state", (frame) =>
        mine(frame) && handlers.scrollState({ atBottom: frame.at_bottom !== false, inCopyMode: frame.in_copy_mode === true, position: Number(frame.scroll_position) || 0 })),
      transport.on("terminal_exit", (frame) => {
        if (!mine(frame)) return;
        this.sessionId = null;
        this.exited = true;
        // The history stays as it was, like the screen.
        this.feed.close();
        handlers.exit(typeof frame.exit_code === "number" ? frame.exit_code : null);
      }),
      transport.on("terminal_error", (frame) => {
        if (!mine(frame)) return;
        // The daemon lost the attachment (it restarted the pane, or forgot us): attach again.
        if (/unknown terminal session|session not found/i.test(String(frame.message ?? ""))) {
          this.sessionId = null;
          this.open();
          return;
        }
        handlers.error(String(frame.message ?? "The terminal stopped."));
      }),
    );
  }

  /** The view's grid; the first one opens the attachment, later ones resize it. */
  resize(cols: number, rows: number): void {
    if (cols < 2 || rows < 2) return;
    const changed = !this.size || this.size.cols !== cols || this.size.rows !== rows;
    this.size = { cols, rows };
    if (this.sessionId && changed) {
      this.send({ type: "terminal_resize", session_id: this.sessionId, cols, rows });
      // The pane reflows: its history comes again at the new width.
      this.feed.output();
    } else this.open();
  }

  /**
   * The terminal is in front of the person again (shown after the chat or another page): its grid becomes the
   * Session's size again, if another viewer (mewla attach on a computer) took it meanwhile. The computer does
   * nothing when the size is already this view's; the history is not asked for again.
   */
  claim(): void {
    if (this.sessionId && this.size) this.send({ type: "terminal_resize", session_id: this.sessionId, ...this.size });
  }

  input(data: string): void {
    if (this.sessionId && data) this.send({ type: "terminal_input", session_id: this.sessionId, data });
  }

  /** History drag: `lines` > 0 is back in time (tmux copy mode). */
  scroll(lines: number): void {
    if (this.sessionId && lines) this.send({ type: "terminal_scroll", session_id: this.sessionId, lines });
  }

  scrollToBottom(): void {
    if (this.sessionId) this.send({ type: "terminal_scroll_cancel", session_id: this.sessionId });
  }

  focusPane(col: number, row: number): void {
    if (this.sessionId) this.send({ type: "terminal_focus_pane", session_id: this.sessionId, col, row });
  }

  /** Ask for the history again (after "History could not load"). */
  retryHistory(): void {
    this.feed.retry();
  }

  /** The history the view holds, row for row: its plain text and flags, for search and the prompt jumps. */
  get history(): TerminalHistoryModel {
    return this.feed.model;
  }

  get attached(): boolean {
    return this.sessionId !== null;
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    if (this.sessionId) this.send({ type: "terminal_close", session_id: this.sessionId });
    this.sessionId = null;
    this.feed.close();
    this.stops.splice(0).forEach((stop) => stop());
  }

  private open(): void {
    if (this.closed || this.exited || this.opening || this.sessionId || !this.size || this.transport.state !== "connected") return;
    this.opening = true;
    const previous = openQueues.get(this.transport) ?? Promise.resolve();
    const turn = previous.then(() => this.attach());
    openQueues.set(this.transport, turn.catch(() => {}));
  }

  private attach(): Promise<void> {
    return new Promise<void>((resolve) => {
      if (this.closed || this.transport.state !== "connected" || !this.size) {
        this.opening = false;
        return resolve();
      }
      const done = (action: () => void) => {
        stopOpened();
        stopFailed();
        clearTimeout(timer);
        this.opening = false;
        action();
        resolve();
      };
      const stopOpened = this.transport.on("terminal_opened", (frame) => done(() => {
        const id = typeof frame.session_id === "string" ? frame.session_id : "";
        if (!id) return;
        // Closed while it was opening: give the attachment back.
        if (this.closed) return this.send({ type: "terminal_close", session_id: id });
        this.sessionId = id;
        this.handlers.reset();
        this.feed.attach(id);
        const cols = Number(frame.cols), rows = Number(frame.rows);
        if (this.size && (cols !== this.size.cols || rows !== this.size.rows)) this.send({ type: "terminal_resize", session_id: id, ...this.size });
        // tmux redraws for a focused client, and passes the FocusIn on only to a pane that asked
        // for focus events. Any other backend (the session host) would type it into the program as
        // "[I", so it goes to tmux only: the daemon names the backend it opened (older ones did not).
        const backend = typeof frame.backend === "string" && frame.backend ? frame.backend : "tmux";
        if (backend === "tmux") this.send({ type: "terminal_input", session_id: id, data: "\u001b[I" });
      }));
      const stopFailed = this.transport.on("terminal_error", (frame) => {
        if (frame.session_id) return;
        done(() => this.handlers.error(String(frame.message ?? "The terminal could not open.")));
      });
      const timer = setTimeout(() => done(() => this.handlers.error("The terminal did not open.")), OPEN_TIMEOUT_MS);
      if (!this.send({ type: "terminal_open", target_id: this.targetId, backend: "tmux", ...this.size })) done(() => {});
    });
  }

  private send(frame: Frame): boolean {
    try {
      this.transport.send(frame);
      return true;
    } catch {
      return false;
    }
  }
}
