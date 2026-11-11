import { formatDateDividerLabel } from "./dates";
import type { DateDividerItem, TimelineItem } from "./types";

export type TimelineRow = TimelineItem | DateDividerItem;

/**
 * A chat read top down: a day's label ("Today", "Yesterday", "November 9")
 * above its first row. Rows without a time join the day before them.
 */
export function withDayDividers(items: readonly TimelineItem[]): TimelineRow[] {
  const rows: TimelineRow[] = [];
  let day: string | null = null;
  for (const item of items) {
    const label = formatDateDividerLabel((item as { timestamp?: string }).timestamp);
    if (label && label !== day) {
      day = label;
      rows.push({ type: "date-divider", id: `date-divider:${label}`, label });
    }
    rows.push(item);
  }
  return rows;
}

/** The newest row's time, for the jump button ("3m"). */
export function latestTimestamp(items: readonly TimelineItem[]): string | undefined {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const timestamp = (items[index] as { timestamp?: string }).timestamp;
    if (timestamp && Number.isFinite(Date.parse(timestamp))) return timestamp;
  }
  return undefined;
}

/** How long ago, compactly: 12s, 3m, 2h, 4d; empty without a time. */
export function compactAge(timestamp: string | undefined, nowMs: number): string {
  if (!timestamp) return "";
  const at = Date.parse(timestamp);
  if (!Number.isFinite(at)) return "";
  const elapsed = Math.max(0, Math.floor((nowMs - at) / 1000));
  if (elapsed < 60) return `${Math.max(1, elapsed)}s`;
  const minutes = Math.floor(elapsed / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}
