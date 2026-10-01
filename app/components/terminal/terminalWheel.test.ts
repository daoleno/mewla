import { expect, test } from 'bun:test';
import { encodeTerminalWheel, selectTerminalScrollRoute } from './terminalWheel';

test('selects the pane under the touch and never guesses arrow-key support', () => {
  const panes = [
    { id: '%1', left: 0, top: 0, cols: 40, rows: 20, alternate: false, mouse: true },
    { id: '%2', left: 41, top: 0, cols: 40, rows: 20, alternate: true, mouse: true },
    { id: '%3', left: 0, top: 21, cols: 81, rows: 20, alternate: true, mouse: false },
  ];
  expect(selectTerminalScrollRoute(panes, 10, 10)).toBe('history');
  expect(selectTerminalScrollRoute(panes, 50, 10)).toBe('wheel');
  expect(selectTerminalScrollRoute(panes, 10, 25)).toBe('none');
  expect(selectTerminalScrollRoute(panes, 40, 10)).toBe('none');
  expect(selectTerminalScrollRoute([], 10, 10)).toBe('none');
  panes[1].alternate = false;
  expect(selectTerminalScrollRoute(panes, 50, 10)).toBe('history');
});

test('uses the native encoder for each direction, bounds batches, and preserves wire bytes', () => {
  for (const [up, down] of [
    ['\x1b[<64;12;8M', '\x1b[<65;12;8M'],
    ['\x1b[M`,' + '(', '\x1b[Ma,' + '('],
  ]) {
    const encode = (button: 'wheelUp' | 'wheelDown') => button === 'wheelUp' ? up : down;
    expect(encodeTerminalWheel(-2, encode)).toBe(up.repeat(2));
    expect(encodeTerminalWheel(1, encode)).toBe(down);
    expect(encodeTerminalWheel(100, encode)).toBe(down.repeat(3));
    expect(encodeTerminalWheel(0, encode)).toBe('');
    expect(encodeTerminalWheel(NaN, encode)).toBe('');
    expect(encodeTerminalWheel(1.5, encode)).toBe('');
  }
  expect(encodeTerminalWheel(1, () => '')).toBe('');
});
