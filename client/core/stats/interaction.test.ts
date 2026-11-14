import { describe, expect, test } from "bun:test";
import {
  accessibleDate,
  activityAccessibilityValue,
  activityAction,
  codexWindowAccessibilityLabel,
  dayToOpen,
  modelAccessibilityLabel,
  scrubPhase,
  swipedRange,
} from "./interaction";
import type { DayCell, ModelStat } from "./statsPayload";

const day = (date: string, overrides: Partial<DayCell> = {}): DayCell => ({
  date, totalTokens: 1_250_000, inputTokens: 0, outputTokens: 0, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0,
  cost: 4.25, costProvenance: "reported", sessions: 3, ...overrides,
});

describe("activity chart, read aloud", () => {
  test("the long date, then tokens, sessions and the cost with its source", () => {
    expect(accessibleDate("2026-10-27")).toBe("Tuesday, October 27, 2026");
    expect(activityAccessibilityValue("2026-10-27", day("2026-10-27"))).toBe("Tuesday, October 27, 2026, 1.3M tokens, 3 sessions, $4.25 reported cost");
    expect(activityAccessibilityValue("2026-10-27", day("2026-10-27", { costProvenance: undefined, sessions: 1 }))).toContain("1 session, $4.25 estimated cost");
  });

  test("a day without activity says so, and a date it cannot read stays as it is", () => {
    expect(activityAccessibilityValue("2026-10-26", null)).toBe("Monday, October 26, 2026, No recorded activity");
    expect(accessibleDate("soon")).toBe("soon");
  });

  test("increment and decrement step between days within the series; activate opens a day with activity only", () => {
    const days = [{ day: day("2026-10-25") }, { day: null }, { day: day("2026-10-27") }];
    expect(activityAction("increment", 2, days)).toEqual({ focus: 2 });
    expect(activityAction("increment", 0, days)).toEqual({ focus: 1 });
    expect(activityAction("decrement", 0, days)).toEqual({ focus: 0 });
    expect(activityAction("decrement", 2, days)).toEqual({ focus: 1 });
    expect(activityAction("activate", 1, days)).toBeNull();
    expect(activityAction("activate", 2, days)).toEqual({ open: days[2].day! });
    expect(activityAction("magic", 2, days)).toBeNull();
    expect(activityAction("increment", 0, [])).toBeNull();
  });

  test("a tap opens only a day that has activity", () => {
    const days = [{ day: null }, { day: day("2026-10-27") }];
    expect(dayToOpen(days, 0)).toBeNull();
    expect(dayToOpen(days, 1)?.date).toBe("2026-10-27");
    expect(dayToOpen(days, 5)).toBeNull();
  });
});

describe("a finger on the chart", () => {
  test("a scrub once it moves across, a page scroll once it moves down first; then it stays", () => {
    expect(scrubPhase("pending", 3, 2)).toBe("pending");
    expect(scrubPhase("pending", 7, 2)).toBe("scrub");
    expect(scrubPhase("pending", 2, 11)).toBe("scroll");
    expect(scrubPhase("pending", -8, 1)).toBe("scrub");
    expect(scrubPhase("scrub", 0, 40)).toBe("scrub");
    expect(scrubPhase("scroll", 40, 0)).toBe("scroll");
  });
});

describe("swiping between ranges", () => {
  test("leftwards goes to the next range, rightwards to the previous, never past the ends", () => {
    expect(swipedRange("fortnight", -80, 4)).toBe("month");
    expect(swipedRange("fortnight", 80, 4)).toBe("week");
    expect(swipedRange("month", -80, 0)).toBeNull();
    expect(swipedRange("week", 80, 0)).toBeNull();
    // A computer with 7 and 30 only: one swipe between them.
    const old = [{ key: "week", label: "7 days", days: 7 }, { key: "month", label: "30 days", days: 30 }] as const;
    expect(swipedRange("week", -80, 0, old)).toBe("month");
  });

  test("a short or mostly vertical drag is not a swipe", () => {
    expect(swipedRange("week", -20, 0)).toBeNull();
    expect(swipedRange("week", -80, 120)).toBeNull();
  });
});

describe("labels", () => {
  test("a model row names the model, its provider, the cost with its source and the activity", () => {
    const model: ModelStat = { name: "gpt-6", id: "gpt-6", provider: "openai", cost: 2.5, costProvenance: "estimated", totalTokens: 250_000, inputTokens: 0, outputTokens: 0, reasoningTokens: 0, cacheRead: 0, cacheCreate: 0, sessions: 1 };
    expect(modelAccessibilityLabel(model)).toBe("gpt-6, openai, Estimated cost $2.50, 250K tokens · 1 session");
  });

  test("a Codex window is one sentence", () => {
    expect(codexWindowAccessibilityLabel("5 hours", 70.4, "Resets Oct 27, 8:05 PM")).toBe("5 hours, 70 percent remaining, Resets Oct 27, 8:05 PM");
  });
});
