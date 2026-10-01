import { buildTerminalPalette, type TerminalThemePalette } from '../../constants/terminalThemes';
import { createTerminal, destroyTerminal, writeData, getVisibleHtml, setTheme } from '../../modules/zen-terminal-vt/src';
import { TERMINAL_HISTORY_MAX_HTML } from './terminalHistory';
import { createTerminalHistoryDecodeBudget } from './terminalHistoryDecodeBudget';

/** Same Ghostty parser and cell HTML formatter as the live grid. The second
 * instance is bounded to 64 rows and never receives PTY input or mouse events.
 * Preserve SGR across page boundaries; yield after 4 ms of page work for input.
 */
export async function renderTerminalHistory(
  rows: readonly string[],
  cols: number,
  theme: TerminalThemePalette,
  current: () => boolean,
): Promise<string[] | null> {
  if (!rows.length) return [];
  const pageSize = 64;
  const handle = createTerminal(cols, pageSize);
  if (!handle) throw new Error('History renderer could not start.');
  const result: string[] = [];
  let bytes = 0;
  const yieldIfNeeded = createTerminalHistoryDecodeBudget();
  try {
    setTheme(handle, { foreground: theme.foreground, background: theme.background,
      cursor: theme.cursor, palette: buildTerminalPalette(theme) });
    for (let offset = 0; offset < rows.length; offset += pageSize) {
      if (!current()) return null;
      const page = rows.slice(offset, offset + pageSize);
      // Clear/home without resetting SGR inherited from the preceding page.
      writeData(handle, '\x1b[2J\x1b[H' + page.join('\r\n'));
      const html = getVisibleHtml(handle);
      const rendered = html.match(/<div class="terminal-row"[^>]*>[\s\S]*?<\/div>/g);
      if (!rendered || rendered.length < page.length) throw new Error('Incomplete Ghostty history render.');
      for (const row of rendered.slice(0, page.length)) {
        bytes += row.length * 2;
        if (bytes > TERMINAL_HISTORY_MAX_HTML) throw new Error('Styled history exceeds the device memory limit.');
        result.push(row);
      }
      if (offset + pageSize < rows.length) {
        const pause = yieldIfNeeded();
        if (pause) await pause;
      }
    }
    return result;
  } finally {
    destroyTerminal(handle);
  }
}
