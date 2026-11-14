import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import type { BrainCurrentWork } from "./types";
import { workCardModel } from "./workCardModel";

const event = (patch: Partial<BrainWorkResultEvent> = {}): BrainWorkResultEvent => ({
  event_id: "e1", kind: "session.done", work_id: "w1", work_title: "Ship the docs", summary: "Docs are live.", session_id: "s1", session_name: "docs",
  occurred_at: "2026-11-12T11:20:00.000Z", unread: true, review_state: "resolved", session_state: "finalized", current_result: true, ...patch,
});
const work = (patch: Partial<BrainCurrentWork> = {}): BrainCurrentWork => ({ work_id: "w1", revision: 1, title: "Ship the docs", status: "done", unread_result: true, ...patch });

describe("a Work in Brain's thread", () => {
  test("came back and new: a result card with its typed next steps; read: its quiet line", () => {
    const actions = [{ kind: "pull_request" as const, url: "https://github.com/o/r/pull/7" }];
    const card = workCardModel({ event: event({ actions }), currentWork: work() });
    expect(card).toMatchObject({ kind: "result", state: "Done", sessionId: "s1", unread: true, actions });
    expect(workCardModel({ event: event(), currentWork: work() }).actions).toEqual([]);
    expect(workCardModel({ event: event(), currentWork: work({ unread_result: false }) }).kind).toBe("line");
    // Left current Work once read: the event's own flag.
    expect(workCardModel({ event: event({ unread: false }) }).kind).toBe("line");
    expect(workCardModel({ event: event({ kind: "session.failed" }) }).state).toBe("Failed");
  });

  test("a new attempt runs: the live card; waiting on a decision: Needs you, which is a request", () => {
    expect(workCardModel({ event: event(), currentWork: work({ status: "running", attempt_session_id: "s2", summary: "Building" }) })).toMatchObject({ kind: "live", state: "Running", sessionId: "s2", summary: "Building" });
    expect(workCardModel({ event: event(), currentWork: work({ status: "needs_input", question: "Publish now?" }) })).toMatchObject({ kind: "needs", summary: "Publish now?" });
    // A Worker's request is Brain's: without Brain's question it asks nothing of you.
    expect(workCardModel({ event: event({ kind: "session.needs_input" }) })).toMatchObject({ kind: "line", summary: "" });
    expect(workCardModel({ event: event({ kind: "session.needs_input" }), currentWork: work({ status: "needs_input" }) })).toMatchObject({ kind: "live", state: "Reviewing" });
  });

  test("a live card carries its Work's judged progress, as the tray row does", () => {
    expect(workCardModel({ event: event(), currentWork: work({ status: "running", progress: { stage: "moving", percent: 58 } }) }).mark).toEqual({ kind: "percent", percent: 60, tone: "moving" });
    expect(workCardModel({ event: event(), currentWork: work({ status: "waiting", progress: { stage: "last", percent: 99 } }) }).mark).toEqual({ kind: "last" });
    expect(workCardModel({ event: event(), currentWork: work({ status: "running" }) }).mark).toBeUndefined();
    expect(workCardModel({ event: event(), currentWork: work({ progress: { stage: "last", percent: 99 } }) }).mark).toBeUndefined();
  });

  test("the Session that ran it is named without the id the daemon appends", () => {
    expect(workCardModel({ event: event({ session_name: "Hello writer (mh-17b3337d43f38037916f0e5b)" }) }).sessionName).toBe("Hello writer");
  });

  test("a lost or uncertain turn is Brain's to handle: no result card, none of the daemon's words (owner 11-13 19:4x)", () => {
    const lost = event({ kind: "session.uncertain", summary: "Delegated Session ended; outcome is unknown", unread: true });
    for (const current of [undefined, work({ status: "cancelled", unread_result: true })]) {
      const model = workCardModel({ event: lost, currentWork: current });
      expect(model.kind).toBe("line");
      expect(model.summary).not.toContain("outcome is unknown");
    }
    expect(workCardModel({ event: lost, currentWork: work({ status: "needs_input" }) })).toMatchObject({ kind: "live", state: "Reviewing" });
    expect(workCardModel({ event: lost, currentWork: work({ status: "needs_input" }) }).summary).not.toContain("outcome is unknown");
  });
});
