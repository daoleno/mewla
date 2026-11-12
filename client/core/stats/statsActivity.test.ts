import { describe, expect, test } from 'bun:test';
import { activityBarIndexAt, activityBars, buildDailyActivitySeries } from './statsActivity';
import type { DayCell } from './statsPayload';

const NOW = new Date(2026, 9, 27, 9, 30);

function day(date: string, totalTokens: number): DayCell {
  return {
    date,
    totalTokens,
    inputTokens: 0,
    outputTokens: 0,
    reasoningTokens: 0,
    cacheRead: 0,
    cacheCreate: 0,
    cost: 0,
    sessions: 1,
  };
}

describe('buildDailyActivitySeries', () => {
  test('fills every day of the week up to today, keeping gaps as null', () => {
    const series = buildDailyActivitySeries([day('2026-10-22', 5), day('2026-10-27', 9)], 'week', NOW);
    expect(series.map((item) => item.date)).toEqual([
      '2026-10-21', '2026-10-22', '2026-10-23', '2026-10-24', '2026-10-25', '2026-10-26', '2026-10-27',
    ]);
    expect(series[1].day?.totalTokens).toBe(5);
    expect(series[2].day).toBeNull();
    expect(series[6].day?.totalTokens).toBe(9);
  });

  test('gives the Day range the week ending today so it has a trend', () => {
    const series = buildDailyActivitySeries([day('2026-10-27', 9)], 'day', NOW);
    expect(series).toHaveLength(7);
    expect(series[0].date).toBe('2026-10-21');
    expect(series[6].date).toBe('2026-10-27');
  });

  test('spans 31 days for Month and from the first recorded day for All', () => {
    expect(buildDailyActivitySeries([], 'month', NOW)).toHaveLength(31);
    const all = buildDailyActivitySeries([day('2026-10-01', 1), day('2026-10-03', 2)], 'all', NOW);
    expect(all[0].date).toBe('2026-10-01');
    expect(all[all.length - 1].date).toBe('2026-10-27');
  });
});

describe('activity chart geometry', () => {
  const frame = { width: 208, height: 100, gap: 4 };

  test('one bar per day across the width with the gap between them, each as tall as its share of the peak', () => {
    expect(activityBars([0, 50, 100, 25], frame)).toEqual([
      { x: 0, width: 49, height: 0 },
      { x: 53, width: 49, height: 50 },
      { x: 106, width: 49, height: 100 },
      { x: 159, width: 49, height: 25 },
    ]);
  });

  test('keeps an all-zero series flat and an empty one bare', () => {
    expect(activityBars([0, 0], frame).map((bar) => bar.height)).toEqual([0, 0]);
    expect(activityBars([], frame)).toEqual([]);
  });

  test('picks the day under a pointer, clamped to the series', () => {
    expect(activityBarIndexAt(-20, 4, frame)).toBe(0);
    expect(activityBarIndexAt(60, 4, frame)).toBe(1);
    expect(activityBarIndexAt(110, 4, frame)).toBe(2);
    expect(activityBarIndexAt(500, 4, frame)).toBe(3);
    expect(activityBarIndexAt(80, 1, frame)).toBe(0);
  });
});
