import type { TerminalPaneScrollMode } from '../../services/terminalHistory';

export type TerminalScrollRoute = 'history' | 'wheel' | 'none';

// Shared with the WebView: tmux's outer DEC modes describe tmux, so select
// using the pane under the finger. No implicit arrow-key fallback: tmux does
// not expose an application's DEC 1007 opt-in in its pane metadata.
export const TERMINAL_SCROLL_ROUTE_SOURCE = `(panes, col, row) => {
  const pane = panes.find((p) => col >= p.left && col < p.left + p.cols &&
    row >= p.top && row < p.top + p.rows);
  if (!pane) return 'none';
  if (!pane.alternate) return 'history';
  return pane.mouse ? 'wheel' : 'none';
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
