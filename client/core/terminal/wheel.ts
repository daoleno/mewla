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

/** One wheel press at the pane's middle (1-based cells): 64 is up (back in time), 65 down. */
export function wheelPress(pane: Pick<PaneScrollMode, "left" | "top" | "cols" | "rows">, up: boolean): string {
  const col = pane.left + Math.floor(pane.cols / 2) + 1;
  const row = pane.top + Math.floor(pane.rows / 2) + 1;
  return `\u001b[<${up ? 64 : 65};${col};${row}M`;
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
