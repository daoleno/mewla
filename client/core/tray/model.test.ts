import { describe, expect, test } from "bun:test";
import type { Worker } from "../sessions/workers";
import type { BrainCurrentWork } from "../work/types";
import { requestsOf, trayModel, type TrayInput } from "./model";

const NOW = Date.parse("2026-11-12T10:00:00Z");

function worker(id: string, patch: Partial<Worker> = {}): Worker {
  return {
    key: `s1:${id}`, id, serverId: "s1", serverName: "Desk", serverUrl: "ws://x", name: id, status: "running",
    cwd: "/home/me/mewla", command: "claude", provider: "claude", summary: "", last_output_lines: [], updated_at: NOW - 60_000, ...patch,
  };
}

function work(id: string, patch: Partial<BrainCurrentWork> = {}): BrainCurrentWork {
  return { work_id: id, revision: 1, title: `Work ${id}`, status: "running", unread_result: false, ...patch };
}

function input(patch: Partial<TrayInput> = {}): TrayInput {
  return {
    hasServer: true, computerName: "atlas", connection: "connected", pairAgain: false, surface: "chat",
    workers: [], today: [], alerts: [], opened: {}, now: NOW, ...patch,
  };
}

describe("tray model", () => {
  test("All quiet with nothing running", () => {
    const m = trayModel(input());
    expect(m.state).toMatchObject({ kind: "quiet", words: "All quiet", dot: null, ink: false, chevron: true });
    expect(m.title).toBe("All quiet");
  });

  test("the count counts rows: a Work and the Session it runs are one row", () => {
    const m = trayModel(input({
      workers: [worker("a"), worker("b"), worker("c", { status: "done" })],
      currentWork: [work("w1", { attempt_session_id: "b" })],
    }));
    expect(m.running.map((r) => r.key)).toEqual(["session:a", "work:w1"]);
    expect(m.state.words).toBe("2 running");
    expect(m.title).toBe(m.state.words);
    // The Work its one Worker runs opens that Session, with its logo.
    expect(m.running[1]!.target).toEqual({ open: "session", workerId: "b" });
    expect(m.done.map((r) => r.key)).toEqual(["session:c"]);
  });

  test("needs you outranks running; Brain's own prompts count", () => {
    const m = trayModel(input({
      workers: [worker("a"), worker("b", { status: "blocked", prompt: { id: "p1", kind: "permission", provider: "claude", title: "Allow?", request: "git push", options: [{ number: 1, label: "Yes" }, { number: 2, label: "No" }] } })],
      brainHost: { id: "host", prompt: { id: "hp", kind: "permission", provider: "claude", title: "Trust this folder?", options: [{ number: 1, label: "Yes, proceed" }, { number: 2, label: "No, exit" }] } },
    }));
    expect(m.state).toMatchObject({ kind: "needs", words: "2 need you", dot: "seal", ink: true });
    expect(m.needs.map((r) => r.key)).toEqual(["prompt:host:hp", "prompt:b:p1"]);
    expect(m.needs[0]!.where).toBe("brain");
    expect(m.needs[1]!.where).toBe("session");
    // The Session asking you is not also a Running row.
    expect(m.running.map((r) => r.key)).toEqual(["session:a"]);
  });

  test("one-tap only for a plain yes/no", () => {
    const plain = requestsOf({ workers: [worker("a", { approvals: [{ id: "x", tool: "Bash", summary: "rm -rf build", options: [{ id: "y", label: "Allow", scope: "once" }, { id: "n", label: "Deny", scope: "deny" }] }] })], alerts: [], now: NOW });
    expect(plain[0]!.oneTap).toEqual({ kind: "approval", workerId: "a", approvalId: "x", optionId: "y" });
    const always = requestsOf({ workers: [worker("a", { approvals: [{ id: "x", options: [{ id: "y", label: "Allow", scope: "once" }, { id: "s", label: "Always allow", scope: "always" }, { id: "n", label: "Deny", scope: "deny" }] }] })], alerts: [], now: NOW });
    expect(always[0]!.oneTap).toBeUndefined();
    const negativeDefault = requestsOf({ workers: [worker("a", { prompt: { id: "p", kind: "permission", provider: "claude", title: "?", options: [{ number: 1, label: "Yes" }, { number: 2, label: "No", selected: true }] } })], alerts: [], now: NOW });
    expect(negativeDefault[0]!.oneTap).toBeUndefined();
    const choice = requestsOf({ workers: [], brainChoice: { callId: "c1", question: "Which branch?" }, alerts: [], now: NOW });
    expect(choice[0]).toMatchObject({ key: "brain-choice:c1", where: "brain", title: "Which branch?" });
    expect(choice[0]!.oneTap).toBeUndefined();
  });

  test("a Work with several Workers is one row: Brain's mark, \"N Workers\", Work detail; its live Workers are not rows of their own", () => {
    const joined = (id: string, live: boolean) => ({ session_id: id, name: id, provider: "claude", state: live ? "running" as const : "done" as const, live, joined_at: "2026-11-12T09:00:00Z", updated_at: "2026-11-12T09:00:00Z" });
    const m = trayModel(input({
      workers: [worker("a", { status: "done" }), worker("b"), worker("c")],
      currentWork: [work("w1", { attempt_session_id: "c", worker_sessions: [joined("a", false), joined("b", true), joined("c", true)] })],
    }));
    const row = m.running.find((r) => r.key === "work:w1")!;
    expect(row).toMatchObject({ line: "3 Workers", tile: { kind: "brain" }, target: { open: "work", workId: "w1" } });
    expect(m.running.map((r) => r.key)).toEqual(["work:w1"]);
    expect(m.state.words).toBe("1 running");
    // One Worker: the row is that Session's.
    const one = trayModel(input({ workers: [worker("b")], currentWork: [work("w1", { worker_sessions: [joined("b", true)] })] }));
    expect(one.running).toHaveLength(1);
    expect(one.running[0]).toMatchObject({ key: "work:w1", tile: { kind: "agent", agent: "claude" }, target: { open: "session", workerId: "b" } });
  });

  test("a new device asking to be let in is a request: no one-tap, its row opens its sheet", () => {
    const r = requestsOf({ workers: [], alerts: [], devices: [{ id: "r1", name: "Firefox on Linux" }], now: NOW });
    expect(r).toEqual([{ key: "device:r1", kind: "device", where: "device", title: "Approve new device", source: "Firefox on Linux", deviceId: "r1" }]);
    expect(trayModel(input({ devices: [{ id: "r1", name: "Firefox on Linux" }] })).state.words).toBe("1 needs you");
  });

  test("a delegated Session's request shows in Brain's thread", () => {
    const r = requestsOf({ workers: [worker("a", { delegated: true, needs_attention: true, status: "blocked" })], alerts: [], now: NOW });
    expect(r[0]).toMatchObject({ key: "attention:a", where: "brain", kind: "attention" });
  });

  test("connection states win over counts and dim the lists", () => {
    const offline = trayModel(input({ connection: "waiting", diagnosis: "Relay unreachable · 13:40", workers: [worker("a")] }));
    expect(offline.state).toMatchObject({ kind: "offline", words: "Offline", dot: "ring", ink: false });
    expect(offline.notice).toMatchObject({ kind: "offline", title: "atlas is offline", detail: "Relay unreachable · 13:40" });
    expect(offline.notice!.actions.map((a) => a.id)).toEqual(["try_again", "details"]);
    expect(offline.dimmed).toBe(true);
    expect(trayModel(input({ connection: "connecting" })).state.words).toBe("Connecting…");
    const pair = trayModel(input({ connection: "waiting", pairAgain: true }));
    expect(pair.state).toMatchObject({ kind: "pair", words: "Pair again", dot: "seal", chevron: false });
    expect(pair.notice).toMatchObject({ kind: "pair", title: "Device not paired" });
  });

  test("Brain starting and can't start", () => {
    expect(trayModel(input({ surface: "waking" })).state.words).toBe("Starting…");
    const down = trayModel(input({ surface: "host_down", hostProblem: { code: "not_signed_in", executor: "agent", detail: "Cursor is not signed in", suggest_executor: "claude", installed_executors: ["claude", "agent"] } }));
    expect(down.state).toMatchObject({ kind: "cant_start", words: "Can't start", dot: "seal" });
    expect(down.notice).toMatchObject({ title: "Brain can't start", detail: "Cursor is not signed in" });
    expect(down.notice!.actions.map((a) => a.label)).toEqual(["Use claude", "Switch executor"]);
  });

  test("Running is ordered by last opened; queued Work is quiet and not counted", () => {
    const m = trayModel(input({
      workers: [worker("a"), worker("b"), worker("c")],
      opened: { b: NOW - 10, c: NOW - 20 },
      openWork: [{ work_id: "q", title: "Queued", status: "open" }],
    }));
    expect(m.running.slice(0, 3).map((r) => r.key)).toEqual(["session:b", "session:c", "session:a"]);
    expect(m.state.words).toBe("3 running");
  });

  test("Today shows the next three; a running item opens its Session", () => {
    const at = (h: number) => Date.parse(`2026-11-12T${String(h).padStart(2, "0")}:00:00Z`);
    const m = trayModel(input({
      today: [
        { id: "past", title: "Standup", time: "8:00", at: at(8), running: false },
        { id: "run", title: "Brief", time: "9:00", at: at(9), running: true, sessionId: "w9" },
        { id: "n1", title: "Lunch", time: "12:00", at: at(12), running: false },
        { id: "n2", title: "Review", time: "15:00", at: at(15), running: false },
        { id: "n3", title: "Gym", time: "18:00", at: at(18), running: false },
      ],
    }));
    expect(m.today.map((r) => r.key)).toEqual(["today:run", "today:n1", "today:n2"]);
    expect(m.today[0]!.target).toEqual({ open: "session", workerId: "w9" });
    expect(m.todayCount).toBe(5);
  });

  test("a Work whose Session asks you is that Needs you row only, not also a row of its own", () => {
    const asking = worker("f", { name: "Fixer", status: "blocked", needs_attention: true, summary: "Which build command?" });
    const m = trayModel(input({
      workers: [asking],
      currentWork: [work("w1", { title: "Fixer", status: "done", unread_result: true, summary: "Ready", attempt_session_id: "f", updated_at: new Date(NOW - 1000).toISOString() })],
    }));
    expect(m.needs.map((r) => r.key)).toEqual(["attention:f"]);
    expect([...m.running, ...m.done].map((r) => r.key)).toEqual([]);
    const answered = trayModel(input({
      workers: [{ ...asking, needs_attention: false }],
      currentWork: [work("w1", { title: "Fixer", status: "done", unread_result: true, summary: "Ready", attempt_session_id: "f", updated_at: new Date(NOW - 1000).toISOString() })],
    }));
    expect(answered.done.map((r) => r.key)).toEqual(["work:w1"]);
  });

  test("Done shows the unread result; a Work waiting on a decision is a request", () => {
    const m = trayModel(input({
      currentWork: [
        work("read", { status: "done", unread_result: false, summary: "old", updated_at: new Date(NOW - 1000).toISOString() }),
        work("new", { status: "done", unread_result: true, summary: "fresh result", updated_at: new Date(NOW - 1000).toISOString() }),
        work("ask", { status: "needs_input", question: "Ship it?", choices: ["Yes", "No"], attention_since: new Date(NOW - 1000).toISOString() }),
      ],
    }));
    expect(m.needs.map((r) => r.key)).toContain("work:ask");
    expect(m.needs.find((r) => r.key === "work:ask")).toMatchObject({ where: "work", title: "Ship it?" });
    // A read result has left current Work; the unread one is Done's first row, with its dot.
    expect(m.done[0]).toMatchObject({ key: "work:new", unread: true, done: true, line: "fresh result" });
  });

  test("a request answered here leaves the tray and the count at once", () => {
    const asking = worker("b", { status: "blocked", prompt: { id: "p1", kind: "permission", provider: "claude", title: "Allow?", request: "git push", options: [{ number: 1, label: "Yes" }, { number: 2, label: "No" }] } });
    expect(trayModel(input({ workers: [asking] })).state.words).toBe("1 needs you");
    const m = trayModel(input({ workers: [asking], answered: new Set(["prompt:b:p1"]) }));
    expect(m.needs).toEqual([]);
    expect(m.state.kind).not.toBe("needs");
  });

  test("no computer: no state control", () => {
    expect(trayModel(input({ hasServer: false })).state.kind).toBe("none");
  });
});
