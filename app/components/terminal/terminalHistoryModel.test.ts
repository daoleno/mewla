import { describe, expect, test } from 'bun:test';
import type { TerminalHistoryDelta } from '../../services/terminalHistory';
import { TerminalHistoryModel, deltaPanes, validateHistoryDelta } from './terminalHistoryModel';
import { terminalSearchCount } from './terminalSearch';

const delta = (over: Partial<TerminalHistoryDelta> = {}): TerminalHistoryDelta => ({
  pane_id: 'mh-1', cols: 80, rows: 24, alternate: false, mouse: false,
  epoch: 7, reset: true, first: 0, from: 0, next: 3, lines: ['a', 'b', 'c'], ...over,
});
const html = (lines: string[]) => lines.map((l) => `<div class="terminal-row">${l}</div>`);

describe('history model, numbered by the host', () => {
  test('the first answer builds everything, later ones append what is new', () => {
    const model = new TerminalHistoryModel();
    expect(model.cursor()).toEqual({ epoch: 0, next: 0 });
    const first = model.applyDelta(delta({ flags: [4, 0, 1] }), html(['a', 'b', 'c']));
    expect(first).toMatchObject({ reset: true, removed: 0, flags: [4, 0, 1] });
    expect(first && first !== 'resync' && first.rows).toHaveLength(3);
    expect(model.cursor()).toEqual({ epoch: 7, next: 3 });

    const more = delta({ reset: false, from: 3, next: 5, lines: ['d', 'e'] });
    const render = model.applyDelta(more, html(['d', 'e']));
    expect(render).toMatchObject({ reset: false, removed: 0, flags: [0, 0] });
    expect(render && render !== 'resync' && render.rows).toEqual(html(['d', 'e']));
    expect(model.rows).toHaveLength(5);
    expect(model.cursor()).toEqual({ epoch: 7, next: 5 });
  });

  test('rows the host no longer keeps are dropped from the front, once', () => {
    const model = new TerminalHistoryModel();
    model.applyDelta(delta(), html(['a', 'b', 'c']));
    const render = model.applyDelta(
      delta({ reset: false, first: 2, from: 3, next: 4, lines: ['d'] }), html(['d']));
    expect(render).toMatchObject({ removed: 2, reset: false });
    expect(model.lines).toEqual(['c', 'd']);
    expect(model.first).toBe(2);
    expect(model.first + model.rows.length).toBe(model.next);
  });

  test('nothing new is no instruction, and the notice alone does not rebuild', () => {
    const model = new TerminalHistoryModel();
    model.applyDelta(delta(), html(['a', 'b', 'c']));
    expect(model.applyDelta(delta({ reset: false, from: 3, next: 3, lines: [] }), [])).toBeNull();
    expect(model.cursor()).toEqual({ epoch: 7, next: 3 });
  });

  test('a mode change alone reaches the renderer', () => {
    const model = new TerminalHistoryModel();
    model.applyDelta(delta(), html(['a', 'b', 'c']));
    const render = model.applyDelta(
      delta({ reset: false, from: 3, next: 3, lines: [], alternate: true, mouse: false, alt_scroll: true, app_cursor: true }), []);
    expect(render).toMatchObject({ rows: [], removed: 0, reset: false });
    expect(render && render !== 'resync' && render.panes?.[0]).toMatchObject({ alternate: true, alt_scroll: true, app_cursor: true });
  });

  test('a delta that does not follow what is held asks for the history again', () => {
    const model = new TerminalHistoryModel();
    model.applyDelta(delta(), html(['a', 'b', 'c']));
    for (const bad of [
      delta({ reset: false, epoch: 8, from: 3, next: 4, lines: ['d'] }),
      delta({ reset: false, from: 4, next: 5, lines: ['e'] }),
      delta({ reset: false, first: 9, from: 3, next: 4, lines: ['d'] }),
    ]) expect(model.applyDelta(bad, html(bad.lines))).toBe('resync');
    expect(model.applyDelta(delta({ reset: false, from: 3, next: 4, lines: ['d'] }), [])).toBe('resync');
    expect(model.rows).toHaveLength(3);
  });

  test('a reset replaces everything, and a replay rebuilds a fresh renderer', () => {
    const model = new TerminalHistoryModel();
    model.applyDelta(delta({ flags: [4, 0, 0] }), html(['a', 'b', 'c']));
    const reset = model.applyDelta(delta({ epoch: 9, first: 40, from: 40, next: 42, lines: ['x', 'y'] }), html(['x', 'y']));
    expect(reset).toMatchObject({ reset: true, removed: 0 });
    expect(model.lines).toEqual(['x', 'y']);
    expect(model.cursor()).toEqual({ epoch: 9, next: 42 });
    expect(model.replay()).toMatchObject({ reset: true, removed: 0, paneId: 'mh-1' });
    expect(model.replay().rows).toEqual(html(['x', 'y']));
  });

  test('one pane covers the grid, and unknown modes stay unknown', () => {
    expect(deltaPanes(delta())).toEqual([
      { id: 'mh-1', left: 0, top: 0, cols: 80, rows: 24, alternate: false, mouse: false }]);
    expect(deltaPanes(delta({ alt_scroll: false, app_cursor: true }))[0]).toMatchObject({ alt_scroll: false, app_cursor: true });
  });
});

describe('history model, tmux snapshots', () => {
  test('rows retained between snapshots are kept; only the new tail is appended', () => {
    const model = new TerminalHistoryModel();
    const panes = [{ id: '%1', left: 0, top: 0, cols: 80, rows: 24, alternate: false, mouse: false }];
    const first = model.applySnapshot('%1', 80, ['a', 'b', 'c'], html(['a', 'b', 'c']), panes);
    expect(first).toMatchObject({ reset: true, removed: 0 });
    const next = model.applySnapshot('%1', 80, ['b', 'c', 'd'], html(['b', 'c', 'd']), panes);
    expect(next).toMatchObject({ reset: false, removed: 1 });
    expect(next.rows).toEqual(html(['d']));
    expect(model.cursor()).toEqual({ epoch: 0, next: 0 });
    const other = model.applySnapshot('%2', 80, ['x'], html(['x']), panes);
    expect(other).toMatchObject({ reset: true });
  });
});

describe('history delta validation', () => {
  test('bounds what reaches native allocation', () => {
    expect(validateHistoryDelta(delta())).toBeTruthy();
    for (const bad of [
      delta({ cols: 0 }), delta({ cols: 1001 }), delta({ pane_id: '' }),
      delta({ next: 9 }), delta({ first: 5, from: 0 }), delta({ epoch: -1 }),
      delta({ flags: [1] }), delta({ flags: [1, 2, 99] }),
      delta({ lines: Array(6001).fill('x'), next: 6001 }),
      delta({ lines: ['x'.repeat(4 * 1024 * 1024 + 1)], next: 1 }),
      { ...delta(), lines: 'no' as unknown as string[] },
    ]) expect(() => validateHistoryDelta(bad)).toThrow();
  });
});

describe('search count', () => {
  test('shows where the shown row stands among the matches', () => {
    expect(terminalSearchCount({ query: '', total: 0, index: -1 })).toBe('');
    expect(terminalSearchCount({ query: 'x', total: 0, index: -1 })).toBe('0');
    expect(terminalSearchCount({ query: 'x', total: 12, index: 2 })).toBe('3/12');
  });
});
