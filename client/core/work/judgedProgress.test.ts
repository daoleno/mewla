import { describe, expect, test } from "bun:test";
import { progressBackLine, progressMark, progressMarkLabel, progressOutcomes, sameProgress, workProgressMark, type BrainWorkProgress } from "./judgedProgress";

const moving: BrainWorkProgress = { stage: "moving", percent: 42, outcomes: [{ id: "a", text: "Fix merged", weight: 2, state: "done" }, { id: "b", text: "Released", weight: 1, state: "todo" }] };

describe("judged progress", () => {
  test("a running Work shows the judged percentage, rounded to 5", () => {
    expect(progressMark(moving, { running: true })).toEqual({ kind: "percent", percent: 40, tone: "moving" });
    expect(progressMark({ ...moving, percent: 43 }, { running: true })).toEqual({ kind: "percent", percent: 45, tone: "moving" });
    expect(progressMark({ ...moving, percent: 0 }, { running: true })).toEqual({ kind: "percent", percent: 0, tone: "moving" });
  });

  test("no number before a judgment, when not running, or while it waits on a person", () => {
    expect(progressMark(undefined, { running: true })).toBeUndefined();
    expect(progressMark(moving, { running: false })).toBeUndefined();
    expect(progressMark(moving, { running: true, waiting: true })).toBeUndefined();
  });

  test("a Work with no finish line has no number", () => {
    expect(progressMark({ stage: "unmeasured", percent: 30 }, { running: true })).toBeUndefined();
  });

  test("the last stretch is its own mark, never 100", () => {
    expect(progressMark({ stage: "last", percent: 99 }, { running: true })).toEqual({ kind: "last" });
    expect(progressMark({ stage: "moving", percent: 140 }, { running: true })).toEqual({ kind: "percent", percent: 95, tone: "moving" });
  });

  test("a sent-back Work is amber and its line says why", () => {
    const back: BrainWorkProgress = { stage: "back", percent: 43, back: "Two unit tests fail on main" };
    expect(progressMark(back, { running: true })).toEqual({ kind: "percent", percent: 45, tone: "back" });
    expect(progressBackLine(back)).toBe("Two unit tests fail on main");
    expect(progressBackLine(moving)).toBe("");
    expect(progressMarkLabel({ kind: "percent", percent: 43, tone: "back" })).toBe("43 percent, sent back");
  });

  test("outcomes keep the judge's order and drop empty ones; equality looks at what is drawn", () => {
    expect(progressOutcomes({ ...moving, outcomes: [...moving.outcomes!, { id: "c", text: " ", weight: 1, state: "todo" }] }).map((o) => o.id)).toEqual(["a", "b"]);
    expect(sameProgress(moving, { ...moving, judged_at: "later" })).toBe(true);
    expect(sameProgress(moving, { ...moving, percent: 43 })).toBe(false);
    expect(sameProgress(moving, { ...moving, outcomes: [moving.outcomes![0], { ...moving.outcomes![1], state: "doing" }] })).toBe(false);
  });

  test("a Work keeps its mark while Brain reads its result, and loses it while a person has a part", () => {
    const last: BrainWorkProgress = { stage: "last", percent: 99 };
    const back: BrainWorkProgress = { stage: "back", percent: 60, back: "More was asked" };
    // The Worker reported: the Work waits on Brain reading, the last stretch.
    expect(workProgressMark({ status: "waiting", progress: last })).toEqual({ kind: "last" });
    expect(workProgressMark({ status: "waiting", progress: back })).toEqual({ kind: "percent", percent: 60, tone: "back" });
    expect(workProgressMark({ status: "running", progress: moving })).toEqual({ kind: "percent", percent: 40, tone: "moving" });
    // A person's part: a question, a needs-you state or wake, or Brain parking it on something named.
    expect(workProgressMark({ status: "running", progress: moving, question: "Ship it now?" })).toBeUndefined();
    expect(workProgressMark({ status: "needs_input", progress: moving })).toBeUndefined();
    expect(workProgressMark({ status: "waiting", progress: moving, wake: { kind: "user_input", ref: "operator:1" } })).toBeUndefined();
    expect(workProgressMark({ status: "waiting", progress: moving, wait_for: "The owner's pick of the look" })).toBeUndefined();
    // Closed Work has no mark.
    expect(workProgressMark({ status: "done", progress: last })).toBeUndefined();
    expect(workProgressMark({ status: "cancelled", progress: moving })).toBeUndefined();
  });
});
