import { expect, test } from 'bun:test';
import { createTerminalRendererTransport } from './terminalRendererTransport';

test('dispatches only to the bound renderer and rejects scripts queued before reload', async () => {
  const calls: { tag: number; script: string }[] = [];
  const transport = createTerminalRendererTransport(async (tag, script) => {
    calls.push({ tag, script });
  }, () => { throw new Error('unexpected failure'); });
  transport.send('window.painted = true;');
  expect(calls).toHaveLength(0);
  transport.bind(12, 3);
  transport.send('window.painted = true;');
  expect(calls[0].tag).toBe(12);
  const window = { __zenRendererGeneration: 4, painted: false };
  new Function('window', calls[0].script)(window);
  expect(window.painted).toBe(false);
  window.__zenRendererGeneration = 3;
  new Function('window', calls[0].script)(window);
  expect(window.painted).toBe(true);
  transport.clear();
  transport.send('window.painted = false;');
  expect(calls).toHaveLength(1);
  expect(() => transport.bind(NaN, 4)).toThrow();
});

test('a detached view failure cannot invalidate a newer renderer or server session', async () => {
  const rejects: ((error: Error) => void)[] = [];
  const failed: number[] = [];
  const transport = createTerminalRendererTransport(() => new Promise((_, reject) => {
    rejects.push(reject);
  }), (_, generation) => failed.push(generation));
  transport.bind(12, 0);
  transport.send('true;');
  transport.clear();
  transport.bind(14, 0);
  rejects[0](new Error('old view detached'));
  await Promise.resolve();
  expect(failed).toEqual([]);
  transport.send('true;');
  rejects[1](new Error('current view detached'));
  await Promise.resolve();
  expect(failed).toEqual([0]);
  transport.send('true;');
  expect(rejects).toHaveLength(2);
});
