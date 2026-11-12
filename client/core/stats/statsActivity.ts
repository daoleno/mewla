import type { DayCell } from './statsPayload';
import type { StatsRange } from './statsTabs';

// Daily activity series and curve geometry for the Stats Activity chart.

export type ActivityDay = {
  date: string;
  day: DayCell | null;
};

// A single day has no trend, so the Day range draws the week that ends today.
const DAY_RANGE_CONTEXT_DAYS = 7;

export function parseDateOnly(dateStr: string): Date | null {
  const parts = dateStr.split('-').map((part) => Number(part));
  if (parts.length < 3 || parts.some((part) => !Number.isFinite(part))) return null;
  return new Date(parts[0], parts[1] - 1, parts[2], 12, 0, 0, 0);
}

export function dateOnlyString(date: Date): string {
  const year = date.getFullYear();
  const month = `${date.getMonth() + 1}`.padStart(2, '0');
  const day = `${date.getDate()}`.padStart(2, '0');
  return `${year}-${month}-${day}`;
}

function addDays(date: Date, days: number): Date {
  const next = new Date(date);
  next.setDate(next.getDate() + days);
  return next;
}

function dateOnly(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate(), 12, 0, 0, 0);
}

/** One entry per calendar day of the range, ending today; missing days are null. */
export function buildDailyActivitySeries(
  days: readonly DayCell[],
  range: StatsRange,
  now: Date = new Date(),
): ActivityDay[] {
  const byDate = new Map(days.map((day) => [day.date, day]));
  const sortedDates = [...byDate.keys()].sort();
  const latestDataDate = sortedDates.length > 0 ? parseDateOnly(sortedDates[sortedDates.length - 1]) : null;
  const today = dateOnly(now);
  const end =
    latestDataDate && latestDataDate > today && range === 'all'
      ? latestDataDate
      : today;
  let start: Date;
  switch (range) {
    case 'day':
      start = addDays(end, -(DAY_RANGE_CONTEXT_DAYS - 1));
      break;
    case 'week':
      start = addDays(end, -6);
      break;
    case 'month':
      start = addDays(end, -30);
      break;
    case 'all':
    default:
      start = sortedDates.length > 0 ? parseDateOnly(sortedDates[0]) ?? end : end;
      break;
  }

  const out: ActivityDay[] = [];
  for (let cursor = start; cursor <= end; cursor = addDays(cursor, 1)) {
    const date = dateOnlyString(cursor);
    out.push({ date, day: byDate.get(date) ?? null });
  }
  return out;
}

/** The chart's frame: its size and the gap between two bars (SPEC §15 Metrics: 4 pt). */
export type BarFrame = { width: number; height: number; gap: number };

export type ChartBar = { x: number; width: number; height: number };

/**
 * One bar per day, side by side across the width with `gap` between them, each as tall as its share
 * of the peak. A day with nothing has no height; the baseline under the bars stands for it.
 */
export function activityBars(values: readonly number[], frame: BarFrame): ChartBar[] {
  const { width, height, gap } = frame;
  if (values.length === 0 || width <= 0) return [];
  const max = Math.max(...values, 0);
  const slot = (width + gap) / values.length;
  const barWidth = Math.max(1, slot - gap);
  return values.map((value, index) => ({ x: index * slot, width: barWidth, height: max > 0 ? (Math.max(0, value) / max) * height : 0 }));
}

/** The day under a pointer at `x`, clamped to the series. */
export function activityBarIndexAt(x: number, count: number, frame: Pick<BarFrame, "width" | "gap">): number {
  if (count <= 1 || frame.width <= 0) return 0;
  const slot = (frame.width + frame.gap) / count;
  return Math.min(count - 1, Math.max(0, Math.floor(x / slot)));
}
