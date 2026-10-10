import { describe, expect, test } from "bun:test";
import type { BrainCurrentWork } from "../../store/brain";
import { brainCatStatusLine, brainCatTailLabel, brainCatTap, presenceWorkGroup, resolveBrainCatPresence } from "./brainCatState";

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

  test("a reconnect that outlasts a blip reads Reconnecting, from the menu footer's same moment", () => {
    const stalled = resolveBrainCatPresence({ ...connected, connection: "connecting", stalled: true });
    expect(stalled).toEqual({ state: "offline", reconnecting: true });
    expect(brainCatTailLabel(stalled)).toBe("Reconnecting");
    expect(resolveBrainCatPresence({ ...connected, connection: "connecting", stalled: false }).state).toBe("waking");
    expect(brainCatTap({ presence: stalled, counts: { needs: 0, running: 0, back: 0, waiting: 0 } })).toEqual({ kind: "retry" });
  });

  test("between turns: attention, then a delivered result, then delegated Work", () => {
    const needs = work({ work_id: "a", title: "Approve deploy", status: "needs_input" });
    const asked = work({ work_id: "d", title: "Apple agreement", status: "waiting", progress_mode: "waiting", wake: { kind: "user_input", ref: "Apple agreement" } });
    const unread = work({ work_id: "b", title: "Weekly report", status: "done", unread_result: true });
    const delegated = work({ work_id: "c", title: "Regression sweep", status: "waiting", attempt_delegated: true });
    expect(resolveBrainCatPresence({ ...connected, currentWork: [delegated, unread, needs, asked] })).toEqual({
      state: "attention",
      count: 2,
      workIds: ["a", "d"],
    });
    expect(resolveBrainCatPresence({ ...connected, currentWork: [delegated, unread] })).toEqual({
      state: "delivered",
      count: 1,
    });
    expect(resolveBrainCatPresence({ ...connected, currentWork: [delegated] })).toEqual({
      state: "delegating",
      count: 1,
      waiting: true,
    });
  });

  test("delegating counts the Workers in the Work list's Running group", () => {
    const running = (id: string) => work({ work_id: id, attempt_delegated: true });
    const waiting = work({ work_id: "w", status: "waiting", attempt_delegated: true });
    const own = work({ work_id: "o", attempt_delegated: false });
    expect(resolveBrainCatPresence({ ...connected, currentWork: [running("a"), running("b"), running("c"), waiting, own] })).toEqual({
      state: "delegating",
      count: 3,
    });
  });

  test("Work Brain runs itself, or finished and read, leaves the cat asleep", () => {
    expect(resolveBrainCatPresence({ ...connected, currentWork: [work({ attempt_delegated: false })] }).state).toBe("idle");
    expect(resolveBrainCatPresence({ ...connected, currentWork: [work({ status: "done" })] }).state).toBe("idle");
    expect(resolveBrainCatPresence(connected).state).toBe("idle");
  });

  test("between turns, and while the link is down, the cat holds a tail row", () => {
    expect(brainCatTailLabel({ state: "attention" })).toBe("Needs you");
    expect(brainCatTailLabel({ state: "delivered" })).toBe("Brought something back");
    expect(brainCatTailLabel({ state: "delegating" })).toBe("1 Worker running");
    expect(brainCatTailLabel({ state: "delegating", waiting: true })).toBe("Waiting on a Worker");
    expect(brainCatTailLabel({ state: "attention", count: 6 })).toBe("6 need you");
    expect(brainCatTailLabel({ state: "delivered", count: 2 })).toBe("Brought 2 things back");
    expect(brainCatTailLabel({ state: "delegating", count: 3 })).toBe("3 Workers running");
    expect(brainCatTailLabel({ state: "delegating", count: 3, waiting: true })).toBe("Waiting on 3 Workers");
    expect(brainCatTailLabel({ state: "idle" })).toBe("All quiet");
    expect(brainCatTailLabel({ state: "offline" })).toBe("Can't reach your computer");
    expect(brainCatTailLabel({ state: "waking" })).toBe("Waking up");
    for (const state of ["working", "homeless"] as const) {
      expect(brainCatTailLabel({ state })).toBeNull();
    }
  });
});

describe("tapping the cat", () => {
  const quiet = { needs: 0, running: 0, back: 0, waiting: 0 };
  test("asleep, it says how things stand", () => {
    expect(brainCatTap({ presence: { state: "idle" }, counts: { ...quiet, running: 2 } })).toEqual({
      kind: "say",
      text: "All quiet. 2 running, nothing needs you.",
    });
    expect(brainCatStatusLine(quiet)).toBe("All quiet. Nothing out, nothing needs you.");
    expect(brainCatStatusLine({ needs: 2, running: 1, back: 0, waiting: 0 })).toBe("1 running, 2 need you.");
  });
  test("needs you: it jumps to the first Work that needs you", () => {
    expect(brainCatTap({ presence: { state: "attention", count: 2, workIds: ["a", "b"] }, counts: { ...quiet, needs: 2 } })).toEqual({ kind: "open-work", workId: "a" });
  });
  test("working: it says what Brain is doing right now", () => {
    expect(brainCatTap({ presence: { state: "idle" }, turnRunning: true, turnLabel: "Read routing.md", counts: quiet })).toEqual({ kind: "show-turn", text: "Right now: Read routing.md" });
    expect(brainCatTap({ presence: { state: "idle" }, turnRunning: true, counts: quiet })).toEqual({ kind: "show-turn", text: "Thinking it through…" });
  });
  test("offline retries; with no computer it opens pairing; waking just says so", () => {
    expect(brainCatTap({ presence: { state: "offline" }, counts: quiet })).toEqual({ kind: "retry" });
    expect(brainCatTap({ presence: { state: "homeless" }, counts: quiet })).toEqual({ kind: "pair" });
    expect(brainCatTap({ presence: { state: "waking" }, counts: quiet }).kind).toBe("say");
  });
});

describe("presenceWorkGroup", () => {
  test("the tail row points at the group it counts", () => {
    expect(presenceWorkGroup({ state: "delegating", count: 3 })).toBe("running");
    expect(presenceWorkGroup({ state: "delegating", count: 1, waiting: true })).toBe("waiting");
    expect(presenceWorkGroup({ state: "delivered", count: 2 })).toBe("back");
    expect(presenceWorkGroup({ state: "attention", count: 1 })).toBe("needs");
    expect(presenceWorkGroup({ state: "idle" })).toBeNull();
  });
});
