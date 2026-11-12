import { formatDateDividerLabel } from "./dates";
import type { DateDividerItem, TimelineItem } from "./types";

export type TimelineRow = TimelineItem | DateDividerItem;

const labelOf = (item: TimelineItem) => formatDateDividerLabel((item as { timestamp?: string }).timestamp);

/**
 * A chat read top down: a day's label ("Today", "Yesterday", "November 9")
 * above its first row. Rows without a time join the day before them; rows
 * without a time at the top join the first day the thread has, so a thread
 * always opens with its day's label (FIDELITY-AUDIT item 17). A thread with
 * no time at all gets no label: none is invented.
 */
export function withDayDividers(items: readonly TimelineItem[]): TimelineRow[] {
  const rows: TimelineRow[] = [];
  let day: string | null = null;
  for (const item of items) {
    const label = labelOf(item) ?? (day === null ? firstLabel(items) : null);
    if (label && label !== day) {
      day = label;
      rows.push({ type: "date-divider", id: `date-divider:${label}`, label });
    }
    rows.push(item);
  }
  return rows;
}

function firstLabel(items: readonly TimelineItem[]): string | null {
  for (const item of items) {
    const label = labelOf(item);
    if (label) return label;
  }
  return null;
}
