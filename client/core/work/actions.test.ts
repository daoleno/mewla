import { describe, expect, test } from "bun:test";
import { brainWorkActions, brainWorkAskDraft, brainWorkUserAction, brainWorkWaitForLine } from "./actions";
import { BRAIN_WORK_STUCK_AFTER_MS, brainWorkSurface } from "./surface";
import type { BrainCurrentWork } from "./types";

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

  test("Brain's pick: the choice Brain recommends is tagged and is the primary one; the reply is still its text", () => {
    const slip = slipOf(work({ question: "How long a trial?", choices: ["7 days", "14 days"], recommended: "14 days", wake: { kind: "user_input", ref: "x" } }));
    const answers = brainWorkActions(slip).filter((action) => action.kind === "answer");
    expect(answers.map((a) => [a.text, a.pick ?? false, a.primary ?? false])).toEqual([["7 days", false, false], ["14 days", true, true]]);
    // A pick that names no choice (an older daemon would not send one) tags nothing.
    const stray = slipOf(work({ question: "How long?", choices: ["7 days"], recommended: "30 days", wake: { kind: "user_input", ref: "x" } }));
    expect(brainWorkActions(stray).filter((a) => a.pick)).toEqual([]);
  });

  test("a question with no choices is answered in words", () => {
    expect(kinds(work({ question: "Which account?" }))).toEqual(["reply", "snooze", "dismiss"]);
  });

  test("parked on you with nothing asked is a quiet wait, not Needs you: ask Brain, snooze, not needed anymore (owner 2026-11-19)", () => {
    expect(kinds(work({ wake: { kind: "user_input", ref: "Apple account holder resolves the agreement" } }))).toEqual(["ask", "snooze", "dismiss"]);
  });

  test("back: accept closes it, or ask Brain", () => {
    expect(kinds(work({ progress_mode: "ready", attention_state: "queued", unread_result: true, attention_since: hoursAgo(2) }))).toEqual(["close", "ask", "dismiss"]);
  });

  test("closed but unread: mark reviewed or ask Brain", () => {
    expect(kinds(work({ status: "done", unread_result: true }))).toEqual(["read", "ask"]);
  });

  test("running: open the Worker, ask Brain, snooze it, or stop it with a confirmation", () => {
    const item = work({ status: "running", progress_mode: "owned", attempt_session_id: "%9", attempt_delegated: true });
    const workers = [{ id: "%9", name: "w", status: "running", phase: "verifying", summary: "Running go test ./..." }];
    expect(kinds(item, workers)).toEqual(["open", "ask", "snooze", "stop"]);
    // The daemon stops only a delegated Worker's Attempt: no Stop to offer for another.
    expect(kinds({ ...item, attempt_delegated: false }, workers)).toEqual(["open", "ask", "snooze"]);
    // A delegated Attempt the list does not have yet is still stopped by the daemon.
    expect(kinds(item, [])).toEqual(["ask", "snooze", "stop"]);
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
  test("a Work parked for days with nothing asked stays a quiet wait that says what it waits on, never Needs you", () => {
    const slip = slipOf(work({ status: "waiting", wake: { kind: "user_input", ref: "operator:1" }, wait_for: "Apple account holder resolves the agreement", attention_since: hoursAgo(24 * 6) }));
    expect(slip.summary).toBe("Apple account holder resolves the agreement");
    expect(slip.group).toBe("waiting");
  });

  test("an answer nothing came back from for a day is Brain's to settle: still With Brain, never Needs you", () => {
    const slip = slipOf(work({ status: "open", progress_mode: "ready", user_action: { kind: "reply", text: "Keep both", at: hoursAgo(30), admission: "accepted" }, attention_since: hoursAgo(30) }));
    expect(slip).toMatchObject({ group: "running", statusLabel: "With Brain", stuck: "no_decision", summary: "You said “Keep both”" });
  });

  test("a Worker that vanished without a result is Brain's to check (owner 11-13 19:4x), with the person's moves on Work detail", () => {
    const slip = slipOf(work({ status: "needs_input", progress_mode: "ready", attention_reason: "turn_lost", attention_since: hoursAgo(3) }));
    expect(slip).toMatchObject({ group: "running", stuck: "outcome_unknown", status: "running", statusLabel: "Reviewing" });
    expect(brainWorkActions(slip).map((action) => action.kind)).toEqual(["ask", "close", "dismiss"]);
  });

  test("a result back for over a day with no decision stays under Back: Brain is woken for it, the person is not (FreerideOpsBatch, 11-19)", () => {
    const item = work({ progress_mode: "ready", attention_state: "queued", summary: "GHCR: 35/36 deleted", attention_since: new Date(NOW - BRAIN_WORK_STUCK_AFTER_MS - 1).toISOString() });
    const slip = slipOf(item);
    expect(slip).toMatchObject({ group: "back", stuck: "no_decision", summary: "GHCR: 35/36 deleted" });
    expect(slip.status).not.toBe("needs");
    expect(kinds(item)).toEqual(["close", "ask", "dismiss"]);
    const fresh = slipOf({ ...item, attention_since: hoursAgo(2) });
    expect(fresh.group).toBe("back");
  });

  test("a snoozed Work waits until its time, then needs you again", () => {
    const item = work({ question: "Ship it?", wake: { kind: "user_input", ref: "x y" }, snoozed_until: new Date(NOW + 3_600_000).toISOString() });
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

test("Ask Brain quotes the Work", () => {
  expect(brainWorkAskDraft({ title: "sync fix", workId: "w1", summary: "Keep both?" })).toBe("Re: sync fix (work w1)\n> Keep both?\n");
});

describe("a running Calendar run's slip", () => {
  test("opens its Worker but cannot stop it from Brain", () => {
    const slip = {
      workId: "calendar-9f",
      group: "running" as const,
      status: "running" as const,
      statusLabel: "Running",
      title: "运营：从今天的工作里挖稿",
      sessionId: "%217",
      unread: false,
      calendar: { itemId: "43754e56", runId: "run-27" },
    };
    const kinds = brainWorkActions(slip).map((action) => action.kind);
    expect(kinds).toContain("open");
    expect(kinds).not.toContain("stop");
  });
});

describe("Waiting for", () => {
  test("shows what the Work waits on when it says something new", () => {
    expect(brainWorkWaitForLine({ waitFor: "Reviewer approval on the PR", summary: "Shipping" })).toBe("Waiting for: Reviewer approval on the PR");
    expect(brainWorkWaitForLine({ waitFor: "operator:2985", summary: "" })).toBeUndefined();
    expect(brainWorkWaitForLine({ waitFor: "Reviewer approval", summary: "Reviewer approval" })).toBeUndefined();
    expect(brainWorkWaitForLine({ waitFor: "Reviewer approval", question: "Merge?" })).toBeUndefined();
  });
});
