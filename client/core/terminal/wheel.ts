import type { PaneScrollMode } from "./history";

/**
 * A program on the alternate screen has no scrollback in the PTY: the view reports a one-finger drag
 * (`scroll`, `lines` > 0 back in time, a line per row of finger travel, a fling going on as more) and
 * the page turns it into what the program understands, as a terminal's wheel does:
 *
 * - the program asked for mouse reports (Claude Code's fullscreen view): one SGR wheel press per line
 *   (Claude moves one line per press, so its text follows the finger, as Termux's touch scrolling does);
 * - it did not, and has the wheel send cursor keys (DEC mode 1007, Codex's transcript; a host that
 *   reports it): one Up or Down per line, so a pager like `less` follows the finger;
 * - otherwise it has nothing to scroll with, and the drag is not sent anywhere.
 *
 * The old app (tmux) did the first, at most three presses a report, and none of the second; the session
 * host's Sessions have no copy mode.
 */
/** The most one report sends (a fling is many reports; a stalled socket must not queue a flood). */
const MAX_LINES = 30;

export type WheelKind = "mouse" | "keys" | "none";

export function wheelKind(pane: Pick<PaneScrollMode, "alternate" | "mouse" | "alt_scroll"> | undefined): WheelKind {
  if (!pane || !pane.alternate) return "none";
  if (pane.mouse) return "mouse";
  return pane.alt_scroll === true ? "keys" : "none";
}

/** The pane a drag belongs to: the one a wheel can reach (a session host Session has one pane), else the first. */
export function paneForDrag(panes: PaneScrollMode[]): PaneScrollMode | undefined {
  return panes.find((p) => wheelKind(p) !== "none") ?? panes[0];
}

/** How the program asked for mouse reports to be encoded (DEC 1006, 1015, 1005, else X10); SGR when the host does not say. */
export type MouseFormat = "sgr" | "urxvt" | "utf8" | "x10";

export function mouseFormat(pane: Pick<PaneScrollMode, "mouse_format"> | undefined): MouseFormat {
  const format = pane?.mouse_format;
  return format === "urxvt" || format === "utf8" || format === "x10" ? format : "sgr";
}

/**
 * One mouse report at 1-based cell (`col`, `row`): `button` 0 is the left button, 64 and 65 the wheel up
 * and down; `release` ends a click. "" where the encoding cannot reach the cell: X10 carries a position in
 * one byte, and this socket carries text, so only cells up to 95 fit (the phone's grid is narrower).
 */
export function mouseReport(format: MouseFormat, button: number, col: number, row: number, release = false): string {
  if (format === "sgr") return `\u001b[<${button};${col};${row}${release ? "m" : "M"}`;
  const code = release ? 3 : button;
  if (format === "urxvt") return `\u001b[${code + 32};${col};${row}M`;
  const limit = format === "utf8" ? 2015 : 95;
  if (col > limit || row > limit || col < 1 || row < 1) return "";
  return `\u001b[M${String.fromCharCode(code + 32)}${String.fromCharCode(col + 32)}${String.fromCharCode(row + 32)}`;
}

/** One wheel press at the pane's middle (1-based cells): 64 is up (back in time), 65 down. */
export function wheelPress(pane: Pick<PaneScrollMode, "left" | "top" | "cols" | "rows" | "mouse_format">, up: boolean): string {
  const col = pane.left + Math.floor(pane.cols / 2) + 1;
  const row = pane.top + Math.floor(pane.rows / 2) + 1;
  return mouseReport(mouseFormat(pane), up ? 64 : 65, col, row);
}

/**
 * A tap at the grid's cell (`col`, `row`, 0-based) as a click for a program that asked for mouse reports
 * (Claude Code's "Jump to bottom", its new-message pill, any TUI's buttons): press and release of the left
 * button, in the encoding the program asked for. "" when the program takes no clicks.
 */
export function clickBytes(pane: PaneScrollMode | undefined, col: number, row: number): string {
  if (!pane?.mouse || !Number.isInteger(col) || !Number.isInteger(row) || col < 0 || row < 0) return "";
  const format = mouseFormat(pane);
  const press = mouseReport(format, 0, col + 1, row + 1);
  return press ? press + mouseReport(format, 0, col + 1, row + 1, true) : "";
}

/** The cursor key a line of drag is: Up when back in time; application mode (DECCKM) uses SS3. */
export function wheelKey(pane: Pick<PaneScrollMode, "app_cursor">, up: boolean): string {
  return `\u001b${pane.app_cursor ? "O" : "["}${up ? "A" : "B"}`;
}

/** The bytes for the program of one report of `lines` (> 0 back in time): empty when it has nothing to scroll with. */
export function wheelBytes(pane: PaneScrollMode | undefined, lines: number): string {
  const kind = wheelKind(pane);
  if (!pane || kind === "none" || !Number.isInteger(lines) || lines === 0) return "";
  const up = lines > 0;
  const n = Math.min(MAX_LINES, Math.abs(lines));
  return (kind === "keys" ? wheelKey(pane, up) : wheelPress(pane, up)).repeat(n);
}
