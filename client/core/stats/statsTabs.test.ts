import { describe, expect, it } from 'bun:test';
import { availableRanges, INITIAL_STATS_RANGE, rangeDays, rangeEyebrow, shownRange, STATS_RANGE_TABS } from './statsTabs';

describe('Stats range tabs (SPEC §15 Metrics)', () => {
  it('is 7 / 14 / 30 days, opening on 14', () => {
    expect(STATS_RANGE_TABS.map((tab) => tab.label)).toEqual(['7 days', '14 days', '30 days']);
    expect(STATS_RANGE_TABS.map((tab) => tab.key)).toEqual(['week', 'fortnight', 'month']);
    expect(INITIAL_STATS_RANGE).toBe('fortnight');
    expect(rangeEyebrow('fortnight')).toBe('Last 14 days');
    expect(rangeDays('month')).toBe(30);
  });

  it('a computer from before the 14-day range shows 7 and 30 only, on 7', () => {
    const old = { day: {}, week: {}, month: {}, all: {} };
    expect(availableRanges(old).map((tab) => tab.key)).toEqual(['week', 'month']);
    expect(shownRange('fortnight', availableRanges(old))).toBe('week');
    expect(shownRange('month', availableRanges(old))).toBe('month');
  });

  it('before any answer, or with none of them, all three', () => {
    expect(availableRanges(null)).toBe(STATS_RANGE_TABS);
    expect(availableRanges({})).toBe(STATS_RANGE_TABS);
    expect(shownRange('fortnight', availableRanges({ week: {}, fortnight: {}, month: {} }))).toBe('fortnight');
  });
});
