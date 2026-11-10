import type { TerminalPaneScrollMode } from '../../services/terminalHistory';

export type TerminalScrollRoute = 'history' | 'wheel' | 'keys' | 'none';

// Shared with the WebView: tmux's outer DEC modes describe tmux, so select
// using the pane under the finger. The alternate screen with no mouse reports
// scrolls with cursor keys only where the host says the program left the
// wheel-sends-cursor-keys mode (DEC 1007) on, as a desktop terminal does;
// tmux does not expose it, so a pane that does not say stays untouched.
export const TERMINAL_SCROLL_ROUTE_SOURCE = `(panes, col, row) => {
  const pane = panes.find((p) => col >= p.left && col < p.left + p.cols &&
    row >= p.top && row < p.top + p.rows);
  if (!pane) return 'none';
  if (!pane.alternate) return 'history';
  if (pane.mouse) return 'wheel';
  return pane.alt_scroll === true ? 'keys' : 'none';
}`;

export const selectTerminalScrollRoute = new Function(
  `return (${TERMINAL_SCROLL_ROUTE_SOURCE});`,
)() as (panes: TerminalPaneScrollMode[], col: number, row: number) => TerminalScrollRoute;

/** The native encoder follows the attached terminal's requested wire mode.
 * For tmux this is the client mode; tmux translates it to the target pane's
 * SGR 1006 / legacy X10 mode and local coordinates without a child process.
 */
export function encodeTerminalWheel(
  ticks: number,
  encode: (button: 'wheelUp' | 'wheelDown') => string,
): string {
  if (!Number.isFinite(ticks) || !Number.isInteger(ticks) || ticks === 0) return '';
  const event = encode(ticks < 0 ? 'wheelUp' : 'wheelDown');
  return event.repeat(Math.min(3, Math.abs(ticks)));
}

/** The cursor keys the wheel stands for on the alternate screen: a swipe that
 * moves the content down is the up key, in the form the program asked for. */
export function encodeTerminalKeys(ticks: number, appCursor: boolean): string {
  if (!Number.isFinite(ticks) || !Number.isInteger(ticks) || ticks === 0) return '';
  const key = (ticks < 0 ? 'A' : 'B');
  return (appCursor ? '\x1bO' : '\x1b[').concat(key).repeat(Math.min(3, Math.abs(ticks)));
}
