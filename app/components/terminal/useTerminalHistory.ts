import { useCallback, useEffect, useMemo, useRef } from 'react';
import { wsClient } from '../../services/websocket';
import type { TerminalThemePalette } from '../../constants/terminalThemes';
import { historyRows, retainedHistoryOverlap, type TerminalHistoryRender } from './terminalHistory';
import { renderTerminalHistory } from './renderTerminalHistory';

/** Output-driven idle refresh, never a scroll-frame request. One capture and
 * one decoder at a time. Session/renderer invalidation makes late work inert.
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
    rows: [] as string[], pane: '', cols: 0, ansi: '', theme: null as TerminalThemePalette | null,
    render: { rows: [], removed: 0, reset: true, notice: '' } as TerminalHistoryRender,
  });

  const schedule = useCallback(() => {
    const s = state.current;
    if (!s.session || s.busy || s.running || s.timer || !s.pending) return;
    s.timer = setTimeout(async () => {
      s.timer = null;
      if (s.busy || !s.session) return;
      s.running = true;
      s.pending = false;
      const session = s.session;
      const epoch = s.epoch;
      const c = config.current;
      const current = () => s.session === session && s.epoch === epoch && !s.busy;
      try {
        const snapshot = await wsClient.getTerminalHistory(c.serverId, session);
        if (!current()) return;
        const rows = historyRows(snapshot);
        if (snapshot.ansi === s.ansi && snapshot.pane_id === s.pane &&
            snapshot.cols === s.cols && c.theme === s.theme &&
            !snapshot.alternate && !s.render.notice) return;
        const rendered = await renderTerminalHistory(rows, snapshot.cols, c.theme, current);
        if (!rendered || !current()) return;
        const normalized = rendered.map((row) => row.replace(/ data-row="\d+"/, ''));
        const samePane = s.pane === snapshot.pane_id && s.cols === snapshot.cols;
        const overlap = samePane ? retainedHistoryOverlap(s.rows, normalized) : 0;
        s.render = {
          rows: normalized,
          removed: s.rows.length - overlap,
          reset: !samePane || (s.rows.length > 0 && overlap === 0),
          notice: snapshot.alternate ? 'History is unavailable while a full-screen app is active.' : '',
        };
        s.rows = normalized;
        s.pane = snapshot.pane_id;
        s.cols = snapshot.cols;
        s.ansi = snapshot.ansi;
        s.theme = c.theme;
        c.publish(s.render);
      } catch (error) {
        if (current()) {
          s.render = { ...s.render, removed: 0, reset: false,
            notice: 'History could not load. Tap to retry.' };
          c.publish(s.render);
          console.warn('[Terminal history]', error);
        }
      } finally {
        s.running = false;
        if (!current() && s.session) s.pending = true;
        schedule();
      }
    }, 750);
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
    s.rows = [];
    s.pane = '';
    s.cols = 0;
    s.ansi = '';
    s.render = { rows: [], removed: 0, reset: true, notice: session ? 'Loading history…' : '' };
    config.current.publish(s.render);
    schedule();
  }, [schedule]);

  const interaction = useCallback((busy: boolean) => {
    const s = state.current;
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
    config.current.publish({ ...s.render, removed: 0, reset: true });
    schedule();
  }, [schedule]);

  useEffect(() => { output(); }, [theme, output]);
  useEffect(() => () => { attach(null); }, [serverId, attach]);

  return useMemo(() => ({ attach, output, interaction, replay }), [attach, output, interaction, replay]);
}
