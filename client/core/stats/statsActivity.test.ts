import { describe, expect, test } from 'bun:test';
import {
  activityAreaPath,
  activityChartPoints,
  activityCurvePath,
  activityIndexAt,
  buildDailyActivitySeries,
} from './statsActivity';
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
  const frame = { width: 208, height: 108, inset: 4 };

  test('scales values to the peak and spreads days across the width', () => {
    const points = activityChartPoints([0, 50, 100], frame);
    expect(points).toEqual([
      { x: 4, y: 104 },
      { x: 104, y: 54 },
      { x: 204, y: 4 },
    ]);
  });

  test('keeps an all-zero series on the baseline', () => {
    expect(activityChartPoints([0, 0], frame).map((point) => point.y)).toEqual([104, 104]);
  });

  test('places a lone day at the right edge', () => {
    expect(activityChartPoints([3], frame)).toEqual([{ x: 204, y: 4 }]);
  });

  test('picks the nearest day for a pointer position, clamped to the series', () => {
    const points = activityChartPoints([1, 2, 3, 4, 5], frame);
    expect(activityIndexAt(-20, points)).toBe(0);
    expect(activityIndexAt(60, points)).toBe(1);
    expect(activityIndexAt(80, points)).toBe(2);
    expect(activityIndexAt(500, points)).toBe(4);
    expect(activityIndexAt(80, points.slice(0, 1))).toBe(0);
  });

  test('draws a curve that never dips below the baseline after a quiet day', () => {
    const points = activityChartPoints([100, 0, 0, 80], frame);
    const path = activityCurvePath(points);
    const ys = [...path.matchAll(/(-?\d+\.\d),(-?\d+\.\d)/g)].map((match) => Number(match[2]));
    for (const y of ys) {
      expect(y).toBeLessThanOrEqual(104);
      expect(y).toBeGreaterThanOrEqual(4);
    }
  });

  test('closes the area to the baseline', () => {
    const points = activityChartPoints([1, 2], frame);
    expect(activityAreaPath(points, 108).endsWith('L204.0,108.0L4.0,108.0Z')).toBe(true);
    expect(activityAreaPath(points.slice(0, 1), 108)).toBe('');
  });
});
