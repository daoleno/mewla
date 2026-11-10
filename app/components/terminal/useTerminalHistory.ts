import { useCallback, useEffect, useMemo, useRef } from 'react';
import { wsClient } from '../../services/websocket';
import type { TerminalThemePalette } from '../../constants/terminalThemes';
import { isTerminalHistoryDelta } from '../../services/terminalHistory';
import { historyRows, type TerminalHistoryRender } from './terminalHistory';
import { TerminalHistoryModel, validateHistoryDelta } from './terminalHistoryModel';
import { renderTerminalHistory } from './renderTerminalHistory';

/** A session host Session sends only the rows after the cursor, so a short
 * wait after output is cheap; a tmux Session sends its whole snapshot. */
const DELTA_REFRESH_MS = 250;
const SNAPSHOT_REFRESH_MS = 750;

const withoutRowIndex = (rows: string[]) => rows.map((row) => row.replace(/ data-row="\d+"/, ''));

/** Output-driven refresh, never a scroll-frame request. One request and one
 * decoder at a time. Session/renderer invalidation makes late work inert.
 * A numbered (session host) history keeps arriving while the reader scrolls:
 * its rows only ever append, which the renderer takes without moving what the
 * reader is looking at. A snapshot is held back while the reader touches it.
 */
export function useTerminalHistory(
  serverId: string,
  theme: TerminalThemePalette,
  publish: (render: TerminalHistoryRender) => void,
) {
  const config = useRef({ serverId, theme, publish });
  config.current = { serverId, theme, publish };
  const state = useRef({
    session: null as string | null, epoch: 0, busy: false, pending: false,
    running: false, timer: null as ReturnType<typeof setTimeout> | null,
    model: new TerminalHistoryModel(), ansi: '', theme: null as TerminalThemePalette | null,
    notice: false,
  });

  const schedule = useCallback(() => {
    const s = state.current;
    const held = s.busy && !s.model.numbered;
    if (!s.session || held || s.running || s.timer || !s.pending) return;
    s.timer = setTimeout(async () => {
      s.timer = null;
      if ((s.busy && !s.model.numbered) || !s.session) return;
      s.running = true;
      s.pending = false;
      const session = s.session;
      const epoch = s.epoch;
      const c = config.current;
      const current = () => s.session === session && s.epoch === epoch &&
        !(s.busy && !s.model.numbered);
      const emit = (render: TerminalHistoryRender) => {
        s.notice = Boolean(render.notice);
        c.publish(render);
      };
      try {
        if (s.theme && s.theme !== c.theme && s.model.lines.length) {
          // The rows are drawn in the old palette: draw every one again.
          const html = await renderTerminalHistory(s.model.lines, s.model.cols, c.theme, current);
          if (!html || !current()) return;
          s.model.rows = withoutRowIndex(html);
          emit(s.model.replay());
        }
        s.theme = c.theme;
        const response = await wsClient.getTerminalHistory(c.serverId, session, s.model.cursor());
        if (!current()) return;
        if (isTerminalHistoryDelta(response)) {
          const delta = validateHistoryDelta(response);
          const html = await renderTerminalHistory(delta.lines, delta.cols, c.theme, current);
          if (!html || !current()) return;
          const result = s.model.applyDelta(delta, withoutRowIndex(html));
          if (result === 'resync') {
            // It did not follow what is held: ask again from nothing.
            s.model.clear();
            s.pending = true;
          } else if (result) emit(result);
          else if (s.notice) emit({ rows: [], removed: 0, reset: false, notice: '',
            panes: s.model.panes, paneId: s.model.paneId });
          return;
        }
        const rows = historyRows(response);
        if (response.ansi === s.ansi && response.pane_id === s.model.paneId &&
            response.cols === s.model.cols && !s.model.numbered &&
            JSON.stringify(response.panes || []) === JSON.stringify(s.model.panes) && !s.notice) return;
        const html = await renderTerminalHistory(rows, response.cols, c.theme, current);
        if (!html || !current()) return;
        emit(s.model.applySnapshot(response.pane_id, response.cols, rows,
          withoutRowIndex(html), response.panes || []));
        s.ansi = response.ansi;
      } catch (error) {
        if (current()) {
          emit({ rows: [], removed: 0, reset: false, panes: s.model.panes, paneId: s.model.paneId,
            notice: 'History could not load. Tap to retry.' });
          console.warn('[Terminal history]', error);
        }
      } finally {
        s.running = false;
        if (!current() && s.session) s.pending = true;
        schedule();
      }
    }, state.current.model.numbered ? DELTA_REFRESH_MS : SNAPSHOT_REFRESH_MS);
  }, []);

  const output = useCallback(() => {
    state.current.pending = true;
    schedule();
  }, [schedule]);

  const attach = useCallback((session: string | null) => {
    const s = state.current;
    if (s.timer) clearTimeout(s.timer);
    s.timer = null;
    s.epoch++;
    s.session = session;
    s.busy = false;
    s.pending = Boolean(session);
    s.model.clear();
    s.ansi = '';
    s.theme = null;
    s.notice = Boolean(session);
    config.current.publish({ rows: [], removed: 0, reset: true, notice: session ? 'Loading history…' : '' });
    schedule();
  }, [schedule]);

  const interaction = useCallback((busy: boolean) => {
    const s = state.current;
    if (s.model.numbered) return; // its rows append under the reader's finger
    s.busy = busy;
    if (busy) {
      s.epoch++;
      if (s.timer) clearTimeout(s.timer);
      s.timer = null;
    } else schedule();
  }, [schedule]);

  const replay = useCallback(() => {
    const s = state.current;
    // A replacement WebView cannot finish the old renderer's gesture.
    s.epoch++;
    s.busy = false;
    s.pending = Boolean(s.session);
    config.current.publish(s.model.replay());
    schedule();
  }, [schedule]);

  useEffect(() => { output(); }, [theme, output]);
  useEffect(() => () => { attach(null); }, [serverId, attach]);

  return useMemo(() => ({ attach, output, interaction, replay }), [attach, output, interaction, replay]);
}
