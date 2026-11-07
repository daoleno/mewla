import { describe, expect, test } from "bun:test";
import { markWorkOpen, STALE_PRESS_MS, workOpenBreakdown } from "./workOpenTiming";

describe("work open timing", () => {
  test("the breakdown splits one open into press, JS commit and paint", () => {
    expect(workOpenBreakdown({ pressIn: 100, press: 112, commit: 140, frame: 160 })).toEqual({
      pressInToPress: 12,
      pressToCommit: 28,
      commitToFrame: 20,
      total: 60,
    });
  });

  test("an open with no finger-down stamp (a screen-reader press) starts at the press", () => {
    expect(workOpenBreakdown({ press: 10, commit: 14, frame: 30 })?.total).toBe(20);
  });

  test("an unfinished open has no breakdown", () => {
    expect(workOpenBreakdown({ pressIn: 1, press: 2, commit: 3 })).toBeNull();
  });

  test("a press that only made the cat speak does not pair with a sheet opened much later", () => {
    expect(workOpenBreakdown({ pressIn: 0, press: 1, commit: 1 + STALE_PRESS_MS + 1, frame: 20_000 })).toBeNull();
    expect(workOpenBreakdown({ pressIn: 0, press: 1, commit: 1 + STALE_PRESS_MS, frame: 11_000 })?.pressToCommit).toBe(STALE_PRESS_MS);
  });

  test("outside a development build nothing is recorded", () => {
    markWorkOpen("pressIn");
    markWorkOpen("press");
    markWorkOpen("commit");
    markWorkOpen("frame");
    expect((globalThis as { __workOpenTimings?: unknown[] }).__workOpenTimings).toBeUndefined();
  });
});
