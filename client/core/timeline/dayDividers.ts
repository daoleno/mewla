import { dayKeyFromTimestamp, formatDateDividerLabel } from "./dates";
import type { DateDividerItem, TimelineItem } from "./types";

export type TimelineRow = TimelineItem | DateDividerItem;

const timeOf = (item: TimelineItem) => (item as { timestamp?: string }).timestamp;
const dayOf = (item: TimelineItem) => (dayKeyFromTimestamp(timeOf(item)) ? timeOf(item)! : null);

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
    // A day's row is keyed by its date, not its label: at midnight "Today" becomes "Yesterday" while the
    // thread is open, and a key that changes in the middle of a list makes NSH's List lay out every row
    // between it and the end again (the reader's place goes with them).
    const time: string | null = dayOf(item) ?? (day === null ? firstTime(items) : null);
    const key: string | null = time ? dayKeyFromTimestamp(time) : null;
    const label: string | null = time ? formatDateDividerLabel(time) : null;
    if (key && label && key !== day) {
      day = key;
      rows.push({ type: "date-divider", id: `date-divider:${key}`, label });
    }
    rows.push(item);
  }
  return rows;
}

function firstTime(items: readonly TimelineItem[]): string | null {
  for (const item of items) {
    const time = dayOf(item);
    if (time) return time;
  }
  return null;
}
