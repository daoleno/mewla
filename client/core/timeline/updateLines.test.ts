import { describe, expect, test } from "bun:test";
import { type UpdateLineItem, withUpdateLines } from "./updateLines";

const row = (id: string, minute?: number) => ({ type: "message", id, ...(minute === undefined ? {} : { timestamp: new Date(Date.UTC(2026, 10, 16, 8, minute)).toISOString() }) });
const line = (version: string, minute: number): UpdateLineItem => ({
  type: "update-line",
  id: `update:${version}`,
  timestamp: new Date(Date.UTC(2026, 10, 16, 8, minute)).toISOString(),
  version,
  highlights: true,
  notes: true,
});

describe("withUpdateLines", () => {
  test("a line sits after the last row at or before its time, a newer one at the end", () => {
    const rows = [row("a", 1), row("b", 5), row("divider"), row("c", 9)];
    expect(withUpdateLines(rows, [line("0.4.9", 6), line("0.5.0", 20)]).map((r) => r.id)).toEqual(["a", "b", "update:0.4.9", "divider", "c", "update:0.5.0"]);
  });

  test("a line from before the thread is another thread's", () => {
    expect(withUpdateLines([row("a", 10)], [line("0.4.8", 2)]).map((r) => r.id)).toEqual(["a"]);
  });

  test("no rows or no lines: the rows as they are", () => {
    const rows = [row("a", 1)];
    expect(withUpdateLines(rows, [])).toBe(rows);
    expect(withUpdateLines([], [line("0.4.9", 1)])).toEqual([]);
  });
});
