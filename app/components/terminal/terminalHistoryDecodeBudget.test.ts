import { expect, test } from 'bun:test';
import { createTerminalHistoryDecodeBudget } from './terminalHistoryDecodeBudget';

test('cheap pages share a work slice, expensive pages yield for input', async () => {
  let now = 0;
  let yields = 0;
  const budget = createTerminalHistoryDecodeBudget(() => now, async () => { yields++; now += 16; });
  for (now = 1; now < 4; now++) expect(budget()).toBeNull();
  await budget();
  expect(yields).toBe(1);
  expect(budget()).toBeNull();
  now += 10; // A single slow native page must yield before another page starts.
  await budget();
  expect(yields).toBe(2);
  expect(budget()).toBeNull();
});

test('a 6000-row capture does not pay one timer per 64-row page', async () => {
  let now = 0;
  let yields = 0;
  const budget = createTerminalHistoryDecodeBudget(() => now, async () => { yields++; now += 16; });
  for (let page = 0; page < 94; page++) {
    now += 0.5;
    const pause = budget();
    if (pause) await pause;
  }
  expect(yields).toBe(11);
});
