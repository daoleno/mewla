import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import type { BrainCurrentWork } from "./types";
import { workCardModel } from "./workCardModel";
import { workForCards } from "./model";

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

  // The three cards the owner could not read (11-19 20:35): Brain parked each with a note in its own shorthand.
  const parked = [
    ["XcodeLogsViewerHang", "11-20 03:17Z retention run; Trigger c57a7c00 pings the Worker after 04:40Z to collect repair numbers and remove the recorder"],
    ["MewlaWorkReclaim", "Brain rebuilds and restarts the live daemon on main (after MewlaScreenHide commits its follow-up), then checks: no Work re-judged without new evidence is not yet in"],
    ["LooperRequestBoard", "reviewer's delta re-check of #103/#104/#106 (tick 1119l) and merges; then Brain releases Looper and the Worker does the production steps"],
  ] as const;

  test("a parked Work's card never shows Brain's note or code name, never asks; the judge's words once it has them (owner 11-19)", () => {
    for (const [title, wait_for] of parked) {
      const current = work({ title, status: "waiting", unread_result: false, wait_for, wake: { kind: "user_input", ref: wait_for }, progress: { stage: "moving", percent: 88 } });
      for (const kind of ["session.uncertain", "session.done"] as const) {
        const before = workCardModel({ event: event({ kind, work_title: title, summary: "Worker's turn ended without a progress report" }), currentWork: current });
        expect(before).toMatchObject({ kind: "live", state: "Waiting", summary: "Nothing needed from you" });
        expect(JSON.stringify(before)).not.toContain(wait_for);
        // Parked on something else, it keeps its ring: no ring reads as "waits on you".
        expect(before.mark).toEqual({ kind: "percent", percent: 90, tone: "moving" });
      }
      const judged = workCardModel({ event: event({ kind: "session.done", work_title: title }), currentWork: { ...current, shown: { title: "看板改造", line: "等独立评审合并后发布，无需你操作", status: "waiting" } } });
      expect(judged).toMatchObject({ kind: "live", title: "看板改造", summary: "等独立评审合并后发布，无需你操作" });
    }
  });

  test("a closed Work's card says how it ended in the judge's words once the judge read it closed; until then what came back", () => {
    const shown = { title: "文档上线", line: "文档已在新地址上线", status: "done" as const };
    expect(workCardModel({ event: event(), currentWork: work({ shown }) })).toMatchObject({ kind: "result", title: "文档上线", summary: "文档已在新地址上线" });
    // A line written while it ran is not how it ended.
    expect(workCardModel({ event: event(), currentWork: work({ summary: "Docs are live.", shown: { ...shown, line: "等部署", status: "active" } }) })).toMatchObject({ title: "文档上线", summary: "Docs are live." });
  });

  test("a Work no Worker runs now still names its card: open Work reaches the thread too (owner's phone 11-19 22:45)", () => {
    const open = [{ work_id: "w-ci", title: "ZeroGProdCi", status: "waiting" as const, shown: { title: "0G 应用 main 自动部署", line: "无需你操作：dev 上部署已跑通，等 Brain 审阅后再合 main。", status: "waiting" as const } }];
    const works = workForCards([work({ work_id: "w1" })], open);
    expect(works.map((item) => item.work_id)).toEqual(["w1", "w-ci"]);
    const card = workCardModel({ event: event({ work_id: "w-ci", work_title: "ZeroGProdCi", summary: "Deploy workflow on dev (5e29f45, c50d05a, 26ea0b4)" }), currentWork: works[1] });
    expect(card).toMatchObject({ kind: "live", title: "0G 应用 main 自动部署", summary: "无需你操作：dev 上部署已跑通，等 Brain 审阅后再合 main。" });
    // The current Work wins over an open entry of the same Work.
    expect(workForCards([work({ work_id: "w-ci", title: "current" })], open)).toHaveLength(1);
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
