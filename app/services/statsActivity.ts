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

export type ChartFrame = {
  width: number;
  height: number;
  /** Inset on every side so end dots and the stroke are not clipped. */
  inset: number;
};

export type ChartPoint = { x: number; y: number };

export function activityChartPoints(values: readonly number[], frame: ChartFrame): ChartPoint[] {
  const { width, height, inset } = frame;
  if (values.length === 0 || width <= inset * 2) return [];
  const max = Math.max(...values, 0);
  const usableWidth = width - inset * 2;
  const usableHeight = height - inset * 2;
  const step = values.length > 1 ? usableWidth / (values.length - 1) : 0;
  return values.map((value, index) => ({
    x: values.length > 1 ? inset + index * step : width - inset,
    y: inset + (1 - (max > 0 ? Math.max(0, value) / max : 0)) * usableHeight,
  }));
}

/** Index of the point nearest to x. */
export function activityIndexAt(x: number, points: readonly ChartPoint[]): number {
  if (points.length <= 1) return 0;
  const step = points[1].x - points[0].x;
  const index = Math.round((x - points[0].x) / step);
  return Math.min(points.length - 1, Math.max(0, index));
}

const fixed = (value: number) => value.toFixed(1);

/**
 * Monotone cubic curve through the points (Fritsch–Carlson), so the line
 * never overshoots below zero or above a peak between two days.
 */
export function activityCurvePath(points: readonly ChartPoint[]): string {
  if (points.length === 0) return '';
  if (points.length === 1) return `M${fixed(points[0].x)},${fixed(points[0].y)}`;
  const count = points.length;
  const slopes: number[] = [];
  for (let index = 0; index < count - 1; index += 1) {
    slopes.push((points[index + 1].y - points[index].y) / (points[index + 1].x - points[index].x));
  }
  const tangents = points.map((_, index) => {
    if (index === 0) return slopes[0];
    if (index === count - 1) return slopes[count - 2];
    const before = slopes[index - 1];
    const after = slopes[index];
    if (before * after <= 0) return 0;
    return (2 * before * after) / (before + after);
  });
  let path = `M${fixed(points[0].x)},${fixed(points[0].y)}`;
  for (let index = 0; index < count - 1; index += 1) {
    const from = points[index];
    const to = points[index + 1];
    const third = (to.x - from.x) / 3;
    path += `C${fixed(from.x + third)},${fixed(from.y + tangents[index] * third)} ${fixed(to.x - third)},${fixed(to.y - tangents[index + 1] * third)} ${fixed(to.x)},${fixed(to.y)}`;
  }
  return path;
}

/** The curve closed down to the baseline, for the soft fill under it. */
export function activityAreaPath(points: readonly ChartPoint[], baseline: number): string {
  if (points.length < 2) return '';
  const first = points[0];
  const last = points[points.length - 1];
  return `${activityCurvePath(points)}L${fixed(last.x)},${fixed(baseline)}L${fixed(first.x)},${fixed(baseline)}Z`;
}
