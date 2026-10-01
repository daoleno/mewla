import { expect, test } from 'bun:test';
import { createTerminalRenderScheduler } from './terminalRenderScheduler';

function fixture() {
  let now = 0;
  let nextID = 0;
  let value = '';
  const pending = new Map<number, { at: number; callback: () => void }>();
  const sent: { at: number; value: string }[] = [];
  const scheduler = createTerminalRenderScheduler(() => sent.push({ at: now, value }), {
    now: () => now,
    setTimeout(callback, delay) {
      pending.set(++nextID, { at: now + delay, callback });
      return nextID as unknown as ReturnType<typeof setTimeout>;
    },
    clearTimeout(id) { pending.delete(id as unknown as number); },
  });
  return {
    sent, pending, scheduler,
    output(text: string) { value += text; scheduler.schedule(); },
    advance(to: number) {
      while (true) {
        const next = [...pending].find(([, timer]) => timer.at <= to);
        if (!next) break;
        pending.delete(next[0]);
        now = next[1].at;
        next[1].callback();
      }
      now = to;
    },
  };
}

test('isolated echo publishes immediately without an RN animation frame', () => {
  const r = fixture();
  r.output('a');
  r.advance(140);
  r.output('b');
  expect(r.sent).toEqual([{ at: 0, value: 'a' }, { at: 140, value: 'ab' }]);
  expect(r.pending.size).toBe(0);
});

test('a flood retains the latest model and one bounded trailing task', () => {
  const r = fixture();
  for (let t = 0; t <= 50; t++) {
    r.advance(t);
    r.output('x');
    expect(r.pending.size).toBeLessThanOrEqual(1);
  }
  r.advance(64);
  expect(r.sent.map(({ at }) => at)).toEqual([0, 16, 32, 48, 64]);
  expect(r.sent.at(-1)?.value).toBe('x'.repeat(51));
  expect(r.pending.size).toBe(0);
});

test('renderer invalidation cancels pending work and allows an immediate new base', () => {
  const r = fixture();
  r.output('old');
  r.advance(2);
  r.output(' pending');
  r.scheduler.cancel();
  r.advance(3);
  r.scheduler.schedule();
  expect(r.sent.map(({ at }) => at)).toEqual([0, 3]);
  r.advance(100);
  expect(r.sent).toHaveLength(2);
});
