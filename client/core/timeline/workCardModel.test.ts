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
  test("came back and new: a result card; read: its quiet line. Brain's next_action is never shown to a person", () => {
    const card = workCardModel({ event: event({ next_action: "Announce it" }), currentWork: work() });
    expect(card).toMatchObject({ kind: "result", state: "Done", sessionId: "s1", unread: true });
    expect(JSON.stringify(card)).not.toContain("Announce it");
    expect(workCardModel({ event: event(), currentWork: work({ unread_result: false }) }).kind).toBe("line");
    // Left current Work once read: the event's own flag.
    expect(workCardModel({ event: event({ unread: false }) }).kind).toBe("line");
    expect(workCardModel({ event: event({ kind: "session.failed" }) }).state).toBe("Failed");
  });

  test("a new attempt runs: the live card; waiting on a decision: Needs you, which is a request", () => {
    expect(workCardModel({ event: event(), currentWork: work({ status: "running", attempt_session_id: "s2", summary: "Building" }) })).toMatchObject({ kind: "live", state: "Running", sessionId: "s2", summary: "Building" });
    expect(workCardModel({ event: event(), currentWork: work({ status: "needs_input", question: "Publish now?" }) })).toMatchObject({ kind: "needs", summary: "Publish now?" });
    expect(workCardModel({ event: event({ kind: "session.needs_input" }) }).kind).toBe("needs");
  });
});
