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
