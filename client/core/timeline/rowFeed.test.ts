import { describe, expect, test } from "bun:test";
import { rowSteps } from "./rowFeed";
import { keyedSplice } from "./scrollTrace";

const rows = (...ids: string[]) => ids.map((id) => ({ id }));
const range = (from: number, to: number, prefix = "m") => Array.from({ length: to - from }, (_, i) => `${prefix}${from + i}`);

/** What NSH's List re-lays for each step: the keys it removes and inserts again. */
function splices(shown: { id: string }[], next: { id: string }[]) {
  const steps = rowSteps(shown, next);
  const out: { at: number; removed: string[]; inserted: string[] }[] = [];
  let before = shown;
  for (const step of steps) {
    const splice = keyedSplice(before.map((r) => r.id), step.map((r) => r.id));
    if (splice) out.push({ at: splice.at, removed: before.slice(splice.at, splice.at + splice.removed).map((r) => r.id), inserted: step.slice(splice.at, splice.at + splice.inserted).map((r) => r.id) });
    before = step as { id: string }[];
  }
  expect(steps.at(-1)).toBe(next);
  return out;
}
/** No step removes a row only to insert it again: every row that stays keeps its measured size and place. */
const narrow = (list: ReturnType<typeof splices>) => list.every((s) => !s.removed.some((id) => s.inserted.includes(id)));

describe("a chat's rows reach its list one narrow change at a time", () => {
  test("a full window slides: the new event appended, then the oldest removed", () => {
    const shown = rows("day", ...range(0, 240));
    const next = rows("day", ...range(1, 241));
    expect(splices(shown, next)).toEqual([
      { at: 241, removed: [], inserted: ["m240"] },
      { at: 1, removed: ["m0"], inserted: [] },
    ]);
  });

  test("a snapshot slid by several events, the foot row after it", () => {
    const shown = rows("day", ...range(0, 240), "thread-foot");
    const next = rows("day", ...range(7, 247), "thread-foot");
    const list = splices(shown, next);
    expect(list).toHaveLength(2);
    expect(narrow(list)).toBe(true);
  });

  test("older history comes in above while a row changes at the end", () => {
    const shown = rows(...range(50, 100), "working");
    const next = rows(...range(0, 100), "a1");
    const list = splices(shown, next);
    expect(narrow(list)).toBe(true);
    expect(list.map((s) => [s.removed, s.inserted.length])).toEqual([[["working"], 1], [[], 50]]);
  });

  test("a tool run folds into one row while the Working row goes", () => {
    const shown = rows(...range(0, 50), "t1", "t2", "t3", ...range(50, 80), "working");
    const next = rows(...range(0, 50), "t1", ...range(50, 80), "a9");
    const list = splices(shown, next);
    expect(narrow(list)).toBe(true);
    expect(list).toHaveLength(2);
  });

  test("one change in the middle, a plain append and a plain removal go as they are", () => {
    expect(rowSteps(rows("a", "b", "c"), rows("a", "x", "c"))).toHaveLength(1);
    expect(rowSteps(rows("a", "b"), rows("a", "b", "c"))).toHaveLength(1);
    expect(rowSteps(rows("a", "b", "c"), rows("b", "c"))).toHaveLength(1);
  });

  test("a row that moved past the shared rows: one step, never a key twice", () => {
    // "b" left the front and came back at the end (a re-sorted timestamp).
    const shown = rows("a", "b", ...range(0, 20));
    const next = rows("a", ...range(0, 20), "b");
    const steps = rowSteps(shown, next);
    for (const step of steps) expect(new Set(step.map((r) => r.id)).size).toBe(step.length);
    expect(steps.at(-1)).toBe(next);
  });
});
