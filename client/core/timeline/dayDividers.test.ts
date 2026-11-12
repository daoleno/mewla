import { describe, expect, test } from "bun:test";
import { withDayDividers } from "./dayDividers";
import type { TimelineItem } from "./types";

const message = (id: string, timestamp?: string) => ({ type: "message", id, role: "assistant", body: id, ...(timestamp ? { timestamp } : {}) }) as unknown as TimelineItem;
const labels = (rows: ReturnType<typeof withDayDividers>) => rows.map((row) => (row.type === "date-divider" ? `[${(row as { label: string }).label}]` : row.id));

describe("day dividers", () => {
  test("a day's label sits above its first row, once", () => {
    const today = new Date();
    const yesterday = new Date(today.getTime() - 24 * 3_600_000);
    const rows = withDayDividers([message("a", yesterday.toISOString()), message("b", yesterday.toISOString()), message("c", today.toISOString()), message("d")]);
    expect(labels(rows)).toEqual(["[Yesterday]", "a", "b", "[Today]", "c", "d"]);
  });

  test("rows without a time get no divider of their own", () => {
    expect(labels(withDayDividers([message("a"), message("b")]))).toEqual(["a", "b"]);
  });
});
