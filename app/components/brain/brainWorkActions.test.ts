import { describe, expect, test } from "bun:test";
import type { BrainCurrentWork } from "../../store/brain";
import { brainWorkActions, brainWorkAskDraft, brainWorkSnoozeUntil, brainWorkUserAction } from "./brainWorkActions";
import { BRAIN_WORK_STUCK_AFTER_MS, brainWorkSurface } from "./brainWorkSurface";

const NOW = Date.parse("2026-10-08T12:00:00Z");
const hoursAgo = (hours: number) => new Date(NOW - hours * 3_600_000).toISOString();

function work(overrides: Partial<BrainCurrentWork>): BrainCurrentWork {
  return { work_id: "w1", revision: 1, title: "sync-fix", status: "waiting", progress_mode: "waiting", unread_result: false, ...overrides };
}

function slipOf(item: BrainCurrentWork, workers = [] as { id: string; name: string; status: string; phase?: string; summary?: string }[]) {
  const surface = brainWorkSurface([item], workers, NOW);
  return surface.slips[0];
}

const kinds = (item: BrainCurrentWork, workers?: Parameters<typeof slipOf>[1]) =>
  brainWorkActions(slipOf(item, workers)).map((action) => `${action.kind}${action.text ? `:${action.text}` : ""}`);

describe("every Work state has a way forward", () => {
  test("Brain's question: its choices are the buttons, then reply, snooze, dismiss", () => {
    const item = work({ question: "Keep both copies, or the newest edit?", choices: ["Keep both", "Newest wins"], wake: { kind: "user_input", ref: "x" }, attention_since: hoursAgo(1) });
    const slip = slipOf(item);
    expect(slip).toMatchObject({ group: "needs", status: "needs", statusLabel: "Needs you", summary: "Keep both copies, or the newest edit?" });
    expect(kinds(item)).toEqual(["answer:Keep both", "answer:Newest wins", "reply", "snooze", "dismiss"]);
    expect(brainWorkActions(slip)[0].primary).toBe(true);
    expect(brainWorkActions(slip).find((action) => action.kind === "dismiss")?.confirm).toBeTruthy();
  });

  test("a question with no choices is answered in words", () => {
    expect(kinds(work({ question: "Which account?" }))).toEqual(["reply", "snooze", "dismiss"]);
  });

  test("needs you without a question: reply, snooze, not needed anymore", () => {
    expect(kinds(work({ wake: { kind: "user_input", ref: "Apple account holder resolves the agreement" } }))).toEqual(["reply", "snooze", "dismiss"]);
  });

  test("back: accept closes it, or ask Brain", () => {
    expect(kinds(work({ progress_mode: "ready", attention_state: "queued", unread_result: true, attention_since: hoursAgo(2) }))).toEqual(["close", "ask", "dismiss"]);
  });

  test("closed but unread: mark reviewed or ask Brain", () => {
    expect(kinds(work({ status: "done", unread_result: true }))).toEqual(["read", "ask"]);
  });

  test("running: open the Worker, ask Brain, or stop it with a confirmation", () => {
    const item = work({ status: "running", progress_mode: "owned", attempt_session_id: "%9", attempt_delegated: true });
    const workers = [{ id: "%9", name: "w", status: "running", phase: "verifying", summary: "Running go test ./..." }];
    expect(kinds(item, workers)).toEqual(["open", "ask", "stop"]);
    expect(slipOf(item, workers).phase).toBe("verifying");
    expect(brainWorkActions(slipOf(item, workers)).at(-1)?.confirm).toContain("cancelled");
  });

  test("you answered: the slip says so and is with Brain, not needing you again", () => {
    const slip = slipOf(work({ status: "open", progress_mode: "ready", user_action: { kind: "reply", text: "Keep both", at: hoursAgo(0.1), admission: "accepted" } }));
    expect(slip).toMatchObject({ group: "running", statusLabel: "With Brain", summary: "You said “Keep both”" });
    const unsure = slipOf(work({ status: "open", progress_mode: "ready", user_action: { kind: "reply", text: "Keep both", at: hoursAgo(0.1), admission: "uncertain" } }));
    expect(unsure.summary).toContain("may not have reached Brain");
  });
});

test("failed: retry through Brain, or dismiss", () => {
  const item = work({ progress_mode: "ready", attention_state: "queued", attention_reason: "turn_failed", attention_since: hoursAgo(1) });
  expect(slipOf(item)).toMatchObject({ status: "failed", statusLabel: "Failed" });
  expect(kinds(item)).toEqual(["ask", "dismiss"]);
  // A failure left for days is still a failure, not "no decision".
  expect(slipOf({ ...item, attention_since: hoursAgo(72) }).stuck).toBeUndefined();
});

describe("nothing stays stuck", () => {
  test("a Worker that vanished without a result is a decision, not a dead card", () => {
    const slip = slipOf(work({ status: "needs_input", progress_mode: "ready", attention_reason: "turn_lost", attention_since: hoursAgo(3) }));
    expect(slip).toMatchObject({ group: "needs", stuck: "outcome_unknown", status: "warning", statusLabel: "Outcome unknown" });
    expect(brainWorkActions(slip).map((action) => action.kind)).toEqual(["ask", "close", "dismiss"]);
  });

  test("a result back for over a day with no decision moves to Needs you", () => {
    const item = work({ progress_mode: "ready", attention_state: "queued", attention_since: new Date(NOW - BRAIN_WORK_STUCK_AFTER_MS - 1).toISOString() });
    const slip = slipOf(item);
    expect(slip).toMatchObject({ group: "needs", stuck: "no_decision", statusLabel: "No decision" });
    expect(slip.summary).toBe("Back for a day with no decision. Close it, or ask Brain.");
    const fresh = slipOf({ ...item, attention_since: hoursAgo(2) });
    expect(fresh.group).toBe("back");
  });

  test("a snoozed Work waits until its time, then needs you again", () => {
    const item = work({ wake: { kind: "user_input", ref: "x y" }, snoozed_until: new Date(NOW + 3_600_000).toISOString() });
    expect(slipOf(item)).toMatchObject({ group: "waiting", statusLabel: "Snoozed" });
    expect(slipOf({ ...item, snoozed_until: hoursAgo(1) }).group).toBe("needs");
  });
});

test("slip actions map onto the daemon's user actions", () => {
  expect(brainWorkUserAction("answer")).toBe("reply");
  expect(brainWorkUserAction("close")).toBe("close");
  expect(brainWorkUserAction("ask")).toBeNull();
  expect(brainWorkUserAction("open")).toBeNull();
});

test("snooze is tomorrow at nine, and Ask Brain quotes the Work", () => {
  const until = brainWorkSnoozeUntil(new Date(2026, 9, 8, 22, 30));
  expect([until.getDate(), until.getHours(), until.getMinutes()]).toEqual([9, 9, 0]);
  expect(brainWorkAskDraft({ title: "sync fix", workId: "w1", summary: "Keep both?" })).toBe("Re: sync fix (work w1)\n> Keep both?\n");
});
