import { describe, expect, test } from "bun:test";
import type { BrainCurrentWork, BrainWorkerRef } from "../../store/brain";
import { brainWorkGroup, brainWorkSummaryLine, brainWorkSurface } from "./brainWorkSurface";

function work(overrides: Partial<BrainCurrentWork>): BrainCurrentWork {
  return {
    work_id: "w1",
    revision: 1,
    title: "ship-the-beta",
    status: "running",
    progress_mode: "owned",
    unread_result: false,
    ...overrides,
  };
}

// Shapes taken from the live daemon's current_work on 2026-10-07.
const live: BrainCurrentWork[] = [
  work({ work_id: "pipeline", title: "zen-terminal-pipeline-2", status: "waiting", progress_mode: "ready", attention_state: "queued", unread_result: true }),
  work({
    work_id: "release",
    title: "zen-release-next",
    status: "waiting",
    progress_mode: "waiting",
    attention_state: "queued",
    unread_result: true,
    wait_for: "Apple account holder resolves the agreement",
    wake: { kind: "user_input", ref: "Apple account holder resolves the agreement" },
  }),
  work({ work_id: "resource-pressure:20261007T125540Z", title: "Machine resource pressure: elevated", status: "waiting", progress_mode: "ready" }),
  work({ work_id: "reopen", title: "perpetuo-browser-reopen-2", attempt_session_id: "%107", attempt_delegated: true, wait_for: "Session %107", unread_result: true }),
  work({ work_id: "doc", title: "perpetuo-document-experience", status: "waiting", progress_mode: "waiting", unread_result: true, wait_for: "Safe window", wake: { kind: "session_terminal", ref: "%12" } }),
  work({
    work_id: "calendar-777",
    title: "Daily Hacker News Briefing",
    status: "open",
    progress_mode: "waiting",
    wait_for: "Calendar occurrence 6cc6",
    wake: { kind: "calendar_result", ref: "calendar:c4b1:6cc6" },
  }),
  work({ work_id: "closed", title: "old-thing", status: "done" }),
  work({ work_id: "cancelled", title: "dropped", status: "cancelled", unread_result: true }),
];

const workers: BrainWorkerRef[] = [
  { id: "%107", name: "perpetuo-browser-reopen-2 (%107)", status: "running", command: "claude", cwd: "/home/me/acelabs/perpetuo", summary: "Fix v1 passes 7/9 at cpus 0.7" },
];

describe("brainWorkSurface", () => {
  test("groups live Work by what it asks of you, needs-you first", () => {
    const surface = brainWorkSurface(live, workers);
    expect(surface.slips.map((slip) => [slip.workId, slip.group])).toEqual([
      ["release", "needs"],
      ["reopen", "running"],
      ["pipeline", "back"],
      ["doc", "waiting"],
    ]);
    expect(surface.counts).toEqual({ needs: 1, running: 1, back: 1, waiting: 1 });
  });

  test("leaves out closed Work, unrun Calendar occurrences and resource telemetry", () => {
    const ids = brainWorkSurface(live, workers).slips.map((slip) => slip.workId);
    expect(ids).not.toContain("closed");
    expect(ids).not.toContain("cancelled");
    expect(ids).not.toContain("calendar-777");
    expect(ids.some((id) => id.startsWith("resource-pressure:"))).toBe(false);
  });

  test("a running slip names its executor and project and says what the Worker last reported", () => {
    const slip = brainWorkSurface(live, workers).slips.find((item) => item.workId === "reopen");
    expect(slip).toMatchObject({
      title: "perpetuo browser reopen 2",
      who: "Claude Code · perpetuo",
      summary: "Fix v1 passes 7/9 at cpus 0.7",
      status: "running",
      statusLabel: "Working",
      sessionId: "%107",
    });
  });

  test("a needs-you slip says what Brain is waiting on", () => {
    const slip = brainWorkSurface(live, workers).slips[0];
    expect(slip).toMatchObject({ status: "needs", statusLabel: "Needs you", summary: "Apple account holder resolves the agreement" });
    expect(slip.sessionId).toBeUndefined();
  });

  test("a back slip prefers the daemon's outcome summary", () => {
    const surface = brainWorkSurface([work({ status: "waiting", progress_mode: "ready", unread_result: true, summary: "Merged, CI green", updated_at: "2026-10-07T12:00:00Z" })], []);
    expect(surface.slips[0]).toMatchObject({ group: "back", status: "ready", statusLabel: "Ready", summary: "Merged, CI green", updatedAt: "2026-10-07T12:00:00Z" });
  });

  test("Work leaves the surface when its status changes to closed", () => {
    const before = brainWorkSurface([work({ status: "needs_input" })], []);
    const after = brainWorkSurface([work({ status: "done" })], []);
    expect(before.counts.needs).toBe(1);
    expect(after.slips).toEqual([]);
    const unreadDone = brainWorkSurface([work({ status: "done", unread_result: true })], []);
    expect(unreadDone.slips.map((slip) => slip.group)).toEqual(["back"]);
  });

  test("an unreviewed Worker question and a user_input wake both need you", () => {
    expect(brainWorkGroup(work({ status: "needs_input", progress_mode: "waiting" }))).toBe("needs");
    expect(brainWorkGroup(work({ status: "needs_input", attention_state: "reviewing" }))).toBe("running");
    expect(brainWorkGroup(work({ status: "waiting", wake: { kind: "user_input", ref: "x" } }))).toBe("needs");
    expect(brainWorkGroup(work({ status: "waiting", progress_mode: "waiting" }))).toBe("waiting");
  });
});

test("a bare wake reference is not shown as the summary", () => {
  const ref = "operator:29850172-4342-479d-a39d-2d98ae1dc87a";
  const surface = brainWorkSurface([work({ status: "waiting", progress_mode: "waiting", wait_for: ref, wake: { kind: "user_input", ref } })], []);
  expect(surface.slips[0].summary).toBeUndefined();
});

describe("brainWorkSummaryLine", () => {
  test("reads like the mock and drops empty groups", () => {
    expect(brainWorkSummaryLine({ needs: 1, running: 3, back: 0, waiting: 0 })).toBe("1 needs you · 3 running");
    expect(brainWorkSummaryLine({ needs: 2, running: 0, back: 1, waiting: 4 })).toBe("2 need you · 1 back · 4 waiting");
    expect(brainWorkSummaryLine({ needs: 0, running: 0, back: 0, waiting: 0 })).toBeNull();
  });
});
