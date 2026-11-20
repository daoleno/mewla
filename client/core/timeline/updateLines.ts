/**
 * The quiet line an update leaves in Brain's chat ("Updated to 0.4.9 · What's new", core/whatsnew): placed among
 * the thread's rows by its time. A line from before the thread's first row is another thread's and is left out.
 */
export type UpdateLineItem = {
  type: "update-line";
  id: string;
  timestamp: string;
  version: string;
  /** Whether its release has pages; else the line opens its fixes. */
  highlights: boolean;
  /** Whether its release has notes; else the line is only "Updated to 0.4.9", nothing to open. */
  notes: boolean;
};

const timeOf = (row: { timestamp?: string }): number | null => {
  if (!row.timestamp) return null;
  const at = Date.parse(row.timestamp);
  return Number.isFinite(at) ? at : null;
};

/** The rows with each line after the last row at or before its time; rows are walked from the newest end. */
export function withUpdateLines<T extends { type: string; timestamp?: string }>(rows: readonly T[], lines: readonly UpdateLineItem[]): (T | UpdateLineItem)[] {
  if (!lines.length || !rows.length) return rows as (T | UpdateLineItem)[];
  let first: number | null = null;
  for (const row of rows) {
    first = timeOf(row);
    if (first !== null) break;
  }
  if (first === null) return rows as (T | UpdateLineItem)[];
  const out: (T | UpdateLineItem)[] = [...rows];
  // Oldest first, so a later line lands after an earlier one placed at the same row.
  for (const line of [...lines].sort((a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp))) {
    const at = Date.parse(line.timestamp);
    if (!Number.isFinite(at) || at < first) continue;
    let index = out.length;
    while (index > 0) {
      const t = timeOf(out[index - 1] as { timestamp?: string });
      if (t !== null && t <= at) break;
      index -= 1;
    }
    out.splice(index, 0, line);
  }
  return out;
}
