import { describe, expect, test } from "bun:test";
import type { BrainCurrentWork } from "../../store/brain";
import { brainCatTailLabel, resolveBrainCatPresence } from "./brainCatState";

function work(overrides: Partial<BrainCurrentWork>): BrainCurrentWork {
  return {
    work_id: "w1",
    revision: 1,
    title: "Ship the beta",
    status: "running",
    unread_result: false,
    ...overrides,
  };
}

const connected = { hasServer: true, connection: "connected" as const, hydrated: true };

describe("resolveBrainCatPresence", () => {
  test("connection outranks everything: no home, offline, then waking", () => {
    const needs = [work({ status: "needs_input" })];
    expect(resolveBrainCatPresence({ ...connected, hasServer: false, currentWork: needs }).state).toBe("homeless");
    expect(resolveBrainCatPresence({ ...connected, connection: "offline", currentWork: needs }).state).toBe("offline");
    expect(resolveBrainCatPresence({ ...connected, connection: "connecting" }).state).toBe("waking");
    expect(resolveBrainCatPresence({ ...connected, hydrated: false }).state).toBe("waking");
  });

  test("between turns: attention, then a delivered result, then delegated Work", () => {
    const needs = work({ work_id: "a", title: "Approve deploy", status: "needs_input" });
    const unread = work({ work_id: "b", title: "Weekly report", status: "done", unread_result: true });
    const delegated = work({ work_id: "c", title: "Regression sweep", status: "waiting", attempt_delegated: true });
    expect(resolveBrainCatPresence({ ...connected, currentWork: [delegated, unread, needs] })).toEqual({
      state: "attention",
      workTitle: "Approve deploy",
      workId: "a",
    });
    expect(resolveBrainCatPresence({ ...connected, currentWork: [delegated, unread] })).toEqual({
      state: "delivered",
      workTitle: "Weekly report",
      workId: "b",
    });
    expect(resolveBrainCatPresence({ ...connected, currentWork: [delegated] })).toEqual({
      state: "delegating",
      workTitle: "Regression sweep",
      workId: "c",
    });
  });

  test("Work Brain runs itself, or finished and read, leaves the cat asleep", () => {
    expect(resolveBrainCatPresence({ ...connected, currentWork: [work({ attempt_delegated: false })] }).state).toBe("idle");
    expect(resolveBrainCatPresence({ ...connected, currentWork: [work({ status: "done" })] }).state).toBe("idle");
    expect(resolveBrainCatPresence(connected).state).toBe("idle");
  });

  test("only between-turn states get a tail row", () => {
    expect(brainCatTailLabel({ state: "attention" })).toBe("Needs you");
    expect(brainCatTailLabel({ state: "delivered" })).toBe("Brought something back");
    expect(brainCatTailLabel({ state: "delegating" })).toBe("Waiting on Workers");
    for (const state of ["idle", "working", "waking", "offline", "homeless"] as const) {
      expect(brainCatTailLabel({ state })).toBeNull();
    }
  });
});
