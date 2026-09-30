import type { TerminalHistorySnapshot } from '../../services/terminalHistory';
import { describe, expect, test } from 'bun:test';
import { historyRows, retainedHistoryOverlap } from './terminalHistory';

const snapshot: TerminalHistorySnapshot = { pane_id: '%1', cols: 80, total: 3, alternate: false, ansi: 'a\nb\nc\n' };

describe('bounded pane history', () => {
  test('keeps ANSI, CJK, trailing cells and empty physical rows unchanged', () => {
    expect(historyRows({ ...snapshot, ansi: '\x1b[38;2;1;2;3m中文🙂 ┌─┐  \n\n' }))
      .toEqual(['\x1b[38;2;1;2;3m中文🙂 ┌─┐  ', '']);
    expect(historyRows({ ...snapshot, ansi: '' })).toEqual([]);
    expect(historyRows({ ...snapshot, alternate: true })).toEqual([]);
  });

  test('bounds rows, bytes and grid dimensions before native allocation', () => {
    expect(historyRows({ ...snapshot, ansi: 'x\n'.repeat(6000) })).toHaveLength(6000);
    for (const bad of [
      { ...snapshot, ansi: 'x\n'.repeat(6001) },
      { ...snapshot, ansi: 'x'.repeat(4 * 1024 * 1024 + 1) },
      { ...snapshot, cols: 0 }, { ...snapshot, cols: 1001 },
      { ...snapshot, total: -1 },
    ]) expect(() => historyRows(bad)).toThrow();
  });

  test('appends preserve the reader row; bounded eviction subtracts only removed rows', () => {
    expect(retainedHistoryOverlap(['a', 'b', 'c'], ['a', 'b', 'c', 'd'])).toBe(3);
    expect(retainedHistoryOverlap(['a', 'b', 'c'], ['b', 'c', 'd'])).toBe(2);
    expect(retainedHistoryOverlap(['a', 'b', 'c'], ['x', 'y'])).toBe(0);
    expect(retainedHistoryOverlap(['a', 'a', 'b', 'a'], ['a', 'b', 'a', 'c'])).toBe(3);
    expect(retainedHistoryOverlap(['a', 'a', 'a'], ['a', 'a'])).toBe(2);
    expect(retainedHistoryOverlap([], ['a'])).toBe(0);
  });
});
