import { describe, expect, test } from "bun:test";
import type { CalendarItem } from "../calendar/model";
import { testClock } from "../calendar/testClock";
import { brainCurrentWorkLifecycle, brainWorkCalendarOrigins, brainWorkGroup, brainWorkSurface, foldMergedSlips } from "./surface";
import type { BrainCurrentWork, BrainWorkerRef } from "./types";

const clock = testClock("2026-10-27T14:00:00Z", "Asia/Shanghai");

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
  work({ work_id: "pipeline", title: "mewla-terminal-pipeline-2", status: "waiting", progress_mode: "ready", attention_state: "queued", unread_result: true }),
  work({
    work_id: "release",
    title: "mewla-release-next",
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
  { id: "%107", name: "perpetuo-browser-reopen-2 (%107)", status: "running", command: "claude", cwd: "/home/me/acelabs/perpetuo", phase: "working", summary: "Fix v1 passes 7/9 at cpus 0.7" },
];

describe("brainWorkSurface", () => {
  test("groups live Work by what it asks of you, needs-you first", () => {
    const surface = brainWorkSurface(live, workers);
    expect(surface.slips.map((slip) => [slip.workId, slip.group])).toEqual([
      ["reopen", "running"],
      ["pipeline", "back"],
      ["release", "back"],
      ["doc", "waiting"],
    ]);
    expect(surface.counts).toEqual({ needs: 0, running: 1, back: 2, waiting: 1, queued: 0 });
    const asked = brainWorkSurface([work({ work_id: "ask", status: "waiting", question: "Ship it?", choices: ["Ship", "Wait"] }), ...live], workers);
    expect(asked.slips.map((slip) => [slip.workId, slip.group])[0]).toEqual(["ask", "needs"]);
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
      project: "perpetuo",
      summary: "Fix v1 passes 7/9 at cpus 0.7",
      status: "running",
      statusLabel: "Working",
      sessionId: "%107",
    });
  });

  test("a running slip never reads its Session's screen: no report, no footer, the Work's own summary instead", () => {
    const pasted = '"$MEWLA_WORKER_PROGRESS_CMD" worker progress --turn-id t1 --status running';
    const unreported = workers.map((worker) => (worker.id === "%107" ? { ...worker, phase: undefined, summary: pasted } : worker));
    const slip = brainWorkSurface(live, unreported).slips.find((item) => item.workId === "reopen")!;
    expect(slip.summary ?? "").not.toContain("MEWLA_WORKER_PROGRESS");
    const footer = workers.map((worker) => (worker.id === "%107" ? { ...worker, summary: "⏵⏵ bypass permissions on (shift+tab to cycle)" } : worker));
    expect(brainWorkSurface(live, footer).slips.find((item) => item.workId === "reopen")!.summary ?? "").not.toContain("shift+tab");
  });

  test("a Work parked on a user_input wake with nothing asked is Brain's wait, never Needs you; Brain's wait note is never its words (owner 2026-11-19)", () => {
    const slip = brainWorkSurface(live, workers).slips.find((item) => item.workId === "release")!;
    expect(slip.group).not.toBe("needs");
    expect(slip.status).not.toBe("needs");
    // "Apple account holder resolves the agreement" is Brain's note: the judge's line says what it waits on.
    expect(slip.summary).toBeUndefined();
    const judged = brainWorkSurface([work({ status: "waiting", wait_for: "11-20 03:17Z retention run; Trigger c57a7c00 pings the Worker", shown: { title: "Xcode 日志查看器不再卡住", line: "等明早清理跑完再验证，无需你操作", status: "waiting" } })], []).slips[0]!;
    expect(judged).toMatchObject({ title: "Xcode 日志查看器不再卡住", shownLine: "等明早清理跑完再验证，无需你操作", shownStatus: "waiting" });
    expect(judged.summary).toBeUndefined();
    // The MewlaScreenVideo case: Brain's review resolved as a wait on its thread, nothing asked.
    const parked = brainWorkSurface([work({ status: "waiting", progress_mode: "waiting", wait_for: "brain-thread:b1", wake: { kind: "user_input", ref: "brain-thread:b1" }, attention_since: "2026-11-19T03:32:30Z" })], [])
      .slips[0]!;
    expect(parked).toMatchObject({ group: "waiting", statusLabel: "Waiting" });
    expect(parked.summary).toBeUndefined();
    // Its lost turn's reason code is the runtime's, nothing to read.
    expect(brainWorkSurface([work({ status: "waiting", progress_mode: "waiting", summary: "worker_stalled: stopped", wake: { kind: "user_input", ref: "brain-thread:b1" } })], []).slips[0]!.summary).toBeUndefined();
  });

  test("a back slip prefers the daemon's outcome summary", () => {
    const surface = brainWorkSurface([work({ status: "waiting", progress_mode: "ready", unread_result: true, summary: "Merged, CI green", updated_at: "2026-10-07T12:00:00Z" })], []);
    expect(surface.slips[0]).toMatchObject({ group: "back", status: "ready", statusLabel: "Ready", summary: "Merged, CI green", updatedAt: "2026-10-07T12:00:00Z" });
  });

  test("Work leaves the surface when its status changes to closed", () => {
    const before = brainWorkSurface([work({ status: "needs_input", question: "Ship it?" })], []);
    const after = brainWorkSurface([work({ status: "done" })], []);
    expect(before.counts.needs).toBe(1);
    expect(after.slips).toEqual([]);
    const unreadDone = brainWorkSurface([work({ status: "done", unread_result: true })], []);
    expect(unreadDone.slips.map((slip) => slip.group)).toEqual(["back"]);
  });

  test("only Brain's question needs you; a user_input wake alone and a Worker's request are Brain's (owner 2026-11-13, 11-19)", () => {
    expect(brainWorkGroup(work({ status: "needs_input", progress_mode: "waiting" }))).toBe("running");
    expect(brainCurrentWorkLifecycle(work({ status: "needs_input", progress_mode: "waiting" })).label).toBe("Reviewing");
    // Brain's review queued (Brain not on it yet) is still Brain's, never a result under Done (shot 11, isolated run).
    expect(brainWorkGroup(work({ status: "needs_input", attention_state: "queued" }))).toBe("running");
    expect(brainWorkSurface([work({ status: "needs_input", progress_mode: "waiting", question: "Which name?" })], []).slips[0]).toMatchObject({ group: "needs", statusLabel: "Needs you" });
    expect(brainWorkGroup(work({ status: "needs_input", attention_state: "reviewing" }))).toBe("running");
    expect(brainWorkGroup(work({ status: "waiting", wake: { kind: "user_input", ref: "x" } }))).toBe("waiting");
    expect(brainWorkGroup(work({ status: "waiting", wake: { kind: "user_input", ref: "x" }, question: "Which name?" }))).toBe("needs");
    expect(brainWorkGroup(work({ status: "waiting", progress_mode: "waiting" }))).toBe("waiting");
  });
});

test("a bare wake reference is not shown as the summary", () => {
  const ref = "operator:29850172-4342-479d-a39d-2d98ae1dc87a";
  const surface = brainWorkSurface([work({ status: "waiting", progress_mode: "waiting", wait_for: ref, wake: { kind: "user_input", ref } })], []);
  expect(surface.slips[0].summary).toBeUndefined();
});

describe("open Work on the rail", () => {
  test("Work no Worker has yet is Queued; Work waiting on something is Waiting", () => {
    const surface = brainWorkSurface([], [], Date.now(), [
      { work_id: "q1", title: "Ship atlas-notes v1.4", status: "open", progress_mode: "ready" },
      { work_id: "w1", title: "Renew the cert", status: "waiting", progress_mode: "waiting", wait_for: "the registrar's approval email" },
      { work_id: "resource-pressure:mem", title: "Memory pressure", status: "open", progress_mode: "ready" },
      { work_id: "cal", title: "Daily digest", status: "open", progress_mode: "waiting", wake: { kind: "calendar_result" } as never },
    ]);
    expect(surface.slips.map((slip) => [slip.workId, slip.group, slip.statusLabel])).toEqual([
      ["w1", "waiting", "Waiting"],
      ["q1", "queued", "Queued"],
    ]);
    // Brain's wait note is Brain's shorthand, never the slip's words (owner 2026-11-19).
    expect(surface.slips[0].summary).toBeUndefined();
    expect(surface.counts.queued).toBe(1);
  });

  test("a Work in current_work is listed once", () => {
    const surface = brainWorkSurface(
      [{ work_id: "q1", revision: 1, title: "Ship it", status: "running", progress_mode: "owned", unread_result: false }],
      [],
      Date.now(),
      [{ work_id: "q1", title: "Ship it", status: "open", progress_mode: "ready" }],
    );
    expect(surface.slips.map((slip) => slip.workId)).toEqual(["q1"]);
    expect(surface.slips[0].group).toBe("running");
  });
});

describe("Calendar runs on the Work rail", () => {
  const calendarWork = work({
    work_id: "calendar-9f",
    title: "运营：从今天的工作里挖稿",
    status: "waiting",
    progress_mode: "waiting",
    wait_for: "Calendar occurrence 5e1b7a90-0000-4000-8000-000000000000",
    wake: { kind: "calendar_result", ref: "calendar:43754e56:run-27" },
    calendar: { item_id: "43754e56", run_id: "run-27" },
  });
  const item = (status: "running" | "completed"): CalendarItem => ({
    id: "43754e56",
    title: "运营：从今天的工作里挖稿",
    kind: "scheduled_action",
    status: status === "running" ? "running" : "scheduled",
    timezone: "Asia/Shanghai",
    recurrence: "daily",
    next_at: "2026-10-27T20:30:00+08:00",
    created_at: "2026-10-23T13:57:41Z",
    updated_at: "2026-10-27T12:30:00Z",
    revision: 13,
    runs: [{ id: "run-27", status, scheduled_for: "2026-10-27T20:30:00+08:00", started_at: "2026-10-27T12:30:00Z", worker_session: "%217" }],
  });

  test("never shows the run's id as its summary", () => {
    const [slip] = brainWorkSurface([calendarWork], []).slips;
    expect(slip.calendar).toEqual({ itemId: "43754e56", runId: "run-27" });
    expect(slip.summary ?? "").not.toContain("5e1b7a90");
  });

  test("names its schedule and run, and opens the run's Session while it runs", () => {
    const surface = brainWorkCalendarOrigins(brainWorkSurface([calendarWork], []), [item("running")], clock);
    const [slip] = surface.slips;
    expect(slip.calendarLabel).toBe("Oct 27");
    expect(slip.calendarLink).toBe("运营：从今天的工作里挖稿 · Oct 27");
    expect(slip.sessionId).toBe("%217");
    expect(slip.group).toBe("running");
    expect(surface.counts.running).toBe(1);
  });

  test("a finished run keeps its name but no Session to open", () => {
    const [slip] = brainWorkCalendarOrigins(brainWorkSurface([calendarWork], []), [item("completed")], clock).slips;
    expect(slip.calendarLink).toBe("运营：从今天的工作里挖稿 · Oct 27");
    expect(slip.sessionId).toBeUndefined();
  });
});

describe("a Work the person merged into another", () => {
  const now = Date.parse("2026-10-27T14:00:00Z");
  const worker = (session_id: string) => ({ session_id, joined_at: "2026-10-27T13:00:00Z" }) as never;
  const surface = (extra: Partial<BrainCurrentWork>) =>
    brainWorkSurface([
      work({ work_id: "board", status: "running", attempt_session_id: "%1", worker_sessions: [worker("%1")] }),
      work({ work_id: "review", status: "running", attempt_session_id: "%2", worker_sessions: [worker("%2"), worker("%1")], shown: { merged_into: "board" }, ...extra }),
    ], [], now).slips;

  test("shows under it: its row goes, its Workers join the other's once", () => {
    const folded = foldMergedSlips(surface({}));
    expect(folded.map((slip) => slip.workId)).toEqual(["board"]);
    expect(folded[0]!.workerSessions?.map((w) => w.session_id)).toEqual(["%1", "%2"]);
    expect(folded[0]!.merged).toEqual([{ workId: "review", title: expect.any(String) }]);
  });

  test("keeps its own row while it asks the person something or has a result they have not read", () => {
    expect(foldMergedSlips(surface({ question: "Ship it?", choices: ["Yes"] })).map((slip) => slip.workId).sort()).toEqual(["board", "review"]);
    expect(foldMergedSlips(surface({ status: "waiting", attempt_session_id: undefined, unread_result: true })).map((slip) => slip.workId).sort()).toEqual(["board", "review"]);
  });

  test("keeps its own row when what it was merged into is not listed", () => {
    const slips = surface({}).filter((slip) => slip.workId === "review");
    expect(foldMergedSlips(slips).map((slip) => slip.workId)).toEqual(["review"]);
  });
});
