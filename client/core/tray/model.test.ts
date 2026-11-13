import { describe, expect, test } from "bun:test";
import type { Worker } from "../sessions/workers";
import type { BrainCurrentWork } from "../work/types";
import { listTime } from "../sessions/listRow";
import { catState } from "../pets/cat";
import { workWorkerLines } from "../work/workers";
import { backMark, requestsOf, trayModel, type TrayInput } from "./model";
import { workDetailWorkerRow } from "../work/workers";

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
  test("a running Session's row: its words, a live dot and how long it has run; never the terminal's text nor a bare state word", () => {
    const m = trayModel(input({ workers: [
      worker("a", { started_at: NOW - 3 * 3_600_000, running_since: NOW - 12 * 60_000, phase: "working", summary: "⏸ manual mode on · ? for shortcuts", last_output_lines: ["⏸ manual mode on · ? for shortcuts"] }),
      worker("b", { running_since: NOW - 4 * 60_000, phase: "working", summary: "Rerunning the suite" }),
      worker("d", { started_at: NOW - 60 * 60_000, phase: "working", summary: "No run start reported" }),
      worker("c", { status: "unknown", summary: '"$MEWLA_WORKER_PROGRESS_CMD" worker progress', last_output_lines: ['"$MEWLA_WORKER_PROGRESS_CMD" worker progress'] }),
    ] }));
    // Nothing reported but the footer: which agent works where, not "Working" under Running.
    expect(m.running.find((r) => r.key === "session:a")).toMatchObject({ line: "Claude Code · mewla", end: "12m", live: true });
    expect(m.running.find((r) => r.key === "session:b")).toMatchObject({ line: "Rerunning the suite", end: "4m", live: true, progress: 3 });
    // How long this run has gone on, never the Session's age: with no run start, only the live dot.
    expect(m.running.find((r) => r.key === "session:d")).toMatchObject({ end: "", live: true });
    expect(m.idle[0]).toMatchObject({ key: "session:c", line: "Claude Code · mewla" });
    expect(m.idle[0]!.live).toBeUndefined();
    expect(JSON.stringify(m)).not.toContain("manual mode");
    expect(JSON.stringify(m)).not.toContain("MEWLA_WORKER_PROGRESS");
    // A phase still says something.
    expect(trayModel(input({ workers: [worker("v", { phase: "verifying" })] })).running[0]!.line).toBe("Verifying");
  });

  test("the live daemon's Claude Session: a footer for a summary, status unknown, an id in its name", () => {
    const footer = "⏵⏵ bypass permissions on (shift+tab to cycle)";
    const claude = worker("mh-17b3337d43f38037916f0e5b", { name: "Hello writer (mh-17b3337d43f38037916f0e5b)", status: "unknown", summary: footer, last_output_lines: [footer] });
    const joined = { session_id: claude.id, name: claude.name, provider: "claude", state: "running" as const, summary: footer, live: true, joined_at: "2026-11-12T09:00:00Z", updated_at: "2026-11-12T09:00:00Z" };
    const m = trayModel(input({ workers: [claude], openWork: [{ work_id: "q", title: "Write the hello", status: "open", worker_sessions: [joined] }] }));
    const row = m.running.find((r) => r.key === "work:q")!;
    // Never "Queued" under Running, never the footer: the Worker's agent and project.
    expect(row).toMatchObject({ quiet: true, line: "Claude Code · mewla", tile: { kind: "agent", agent: "claude" } });
    expect(m.state.words).toBe("1 queued");
    expect(JSON.stringify(m)).not.toContain("shift+tab");
    expect(JSON.stringify(m)).not.toContain("(mh-");
    // Without a Session to read, a queued Work says it has not started, not "Queued".
    const bare = trayModel(input({ openWork: [{ work_id: "q", title: "Write the hello", status: "open" }] }));
    expect(bare.running[0]!.line).toBe("Not started yet");
    const waiting = trayModel(input({ openWork: [{ work_id: "w", title: "Ship", status: "waiting", wait_for: "the deploy window" }] }));
    expect(waiting.running[0]!.line).toBe("the deploy window");
    // A Session blocked on something unreadable asks "Waiting for you", not its footer.
    const asks = requestsOf({ workers: [{ ...claude, status: "blocked", needs_attention: true }], alerts: [], now: NOW });
    expect(asks[0]).toMatchObject({ title: "Waiting for you", source: "Hello writer" });
  });

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
    // The headline counts both, as the mock's "12 running, 3 need you"; the control says only what needs you.
    expect(m.title).toBe("1 running, 2 need you");
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
    // The line says what its first live Worker does; the end, how long that Worker has worked on it.
    expect(row).toMatchObject({ line: "3 Workers · Claude Code", end: "1h", live: true, tile: { kind: "brain" }, target: { open: "work", workId: "w1" } });
    expect(m.running.map((r) => r.key)).toEqual(["work:w1"]);
    expect(m.state.words).toBe("1 running");
    // A result ends in when it came back, then its check.
    const back = trayModel(input({ currentWork: [work("w2", { status: "done", unread_result: true, updated_at: "2026-11-12T09:30:00Z" })] }));
    expect(back.done.find((r) => r.key === "work:w2")).toMatchObject({ end: listTime(Date.parse("2026-11-12T09:30:00Z"), new Date(NOW)), done: true, unread: true });
    // One Worker: the row is that Session's.
    const one = trayModel(input({ workers: [worker("b")], currentWork: [work("w1", { worker_sessions: [joined("b", true)] })] }));
    expect(one.running).toHaveLength(1);
    expect(one.running[0]).toMatchObject({ key: "work:w1", tile: { kind: "agent", agent: "claude" }, target: { open: "session", workerId: "b" } });
  });

  test("Work detail's Worker is a tray row: its task name without its id (the logo names the agent), what it does or where, \"● 12m\" while it works", () => {
    const footer = "⏵⏵ bypass permissions on (shift+tab to cycle)";
    const id = "mh-17b3337d43f38037916f0e5b";
    const joined = new Date(NOW - 12 * 60_000).toISOString();
    const lines = workWorkerLines([
      { session_id: id, name: `Hello writer (${id})`, provider: "claude", state: "running", summary: footer, live: true, joined_at: joined, updated_at: joined },
      { session_id: "%8", name: "builder", provider: "codex", state: "failed", summary: "Build broke on Windows", live: false, joined_at: joined, ended_at: joined, updated_at: joined },
    ], new Map([[id, worker(id, { name: `Hello writer (${id})`, status: "running", summary: footer })]]), new Date(NOW));
    const [live, failed] = lines.map((line) => workDetailWorkerRow(line, NOW));
    expect(live).toMatchObject({ title: "Hello writer", line: "mewla", end: "12m", live: true, tile: { kind: "agent", agent: "claude" }, target: { open: "session", workerId: id } });
    expect(failed).toMatchObject({ title: "builder", line: "Failed · Build broke on Windows", tile: { kind: "agent", agent: "codex" } });
    expect(failed!.live).toBeUndefined();
    // Under the eyebrow "Work · mewla" the project is not said again.
    expect(workDetailWorkerRow(lines[0]!, NOW, "mewla").line).toBe("");
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

  test("only quiet Work under Running: it says so, soft and without a dot, and the cat naps (the header, the tray and the cat agree)", () => {
    const m = trayModel(input({ openWork: [{ work_id: "q", title: "Queued", status: "open" }, { work_id: "w", title: "Waiting", status: "waiting", wait_for: "the deploy window" }] }));
    expect(m.running.map((r) => r.quiet)).toEqual([true, true]);
    // SPEC §1.2 The count: never "All quiet" over a Running section that lists rows.
    expect(m.state).toMatchObject({ kind: "quiet", words: "1 queued, 1 waiting", dot: null, ink: false });
    expect(m.title).toBe("1 queued, 1 waiting");
    const one = trayModel(input({ openWork: [{ work_id: "q", title: "Queued", status: "open" }] }));
    expect([one.state.words, one.title]).toEqual(["1 queued", "1 queued"]);
    expect(m.runningCount).toBe(0);
    const cat = (t: typeof m) => catState({ kind: t.state.kind, starting: false, needs: t.needs.length, brainTurn: false, running: t.runningCount });
    expect(cat(m)).toBe("idle");
    const busy = trayModel(input({ workers: [worker("a")], openWork: [{ work_id: "q", title: "Queued", status: "open" }] }));
    expect(busy.runningCount).toBe(1);
    expect(busy.state.words).toBe("1 running");
    expect(cat(busy)).toBe("delegating");
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
    // Not "Running": a run whose Session is not listed says when it started.
    expect(m.today[0]!.line).toBe("Started 9:00");
    const listed = trayModel(input({ workers: [worker("w9", { running_since: NOW - 12 * 60_000, phase: "reading", summary: "Reading the news" })], today: [{ id: "run", title: "Brief", time: "9:00", at: at(9), running: true, sessionId: "w9" }] }));
    expect(listed.today[0]).toMatchObject({ line: "Reading the news", end: "12m", live: true });
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

describe("Back's connection mark (SPEC §1.3)", () => {
  test("offline and connecting are grey, Pair again red; any other state has none", () => {
    expect(backMark("offline")).toEqual({ tone: "grey", label: "Back · Offline" });
    expect(backMark("connecting")).toEqual({ tone: "grey", label: "Back · Connecting" });
    expect(backMark("pair")).toEqual({ tone: "red", label: "Back · Pair again" });
    for (const kind of ["quiet", "running", "needs", "starting", "cant_start", "none"] as const) expect(backMark(kind)).toBeNull();
  });
});

describe("the daemon's run fields (WIRE §7, §9)", () => {
  test("a running Session says the call it started last and how long this run has gone, not since it was created", () => {
    const m = trayModel(input({ workers: [
      worker("r", { started_at: NOW - 26 * 3_600_000, running_since: NOW - 3 * 60_000, activity: "Bash curl -sI https://example.org", summary: "Claude working" }),
    ] }));
    expect(m.running[0]).toMatchObject({ key: "session:r", line: "Bash curl -sI https://example.org", end: "3m", live: true });
  });

  test("a call that comes with its parts reads as the thread's tool row does, not the tool's name glued to its input", () => {
    const m = trayModel(input({ workers: [
      worker("r", { running_since: NOW - 60_000, activity: "Bash ping -c 50 127.0.0.1", activity_tool: "Bash", activity_summary: "ping -c 50 127.0.0.1" }),
      worker("f", { running_since: NOW - 60_000, activity: "Read /etc/hostname", activity_tool: "Read", activity_summary: "/etc/hostname" }),
    ] }));
    expect(m.running.find((r) => r.key === "session:r")!.line).toBe("Run ping -c 50 127.0.0.1");
    expect(m.running.find((r) => r.key === "session:f")!.line).not.toMatch(/^Read \/etc/);
  });

  test("a prompt's tray row is titled with the call the daemon names", () => {
    const prompt = { id: "p1", kind: "permission", provider: "claude", title: "Waiting for Claude permission", tool: "Read", summary: "/etc/hostname",
      request: "Read file\nRead(/etc/hostname)\nDo you want to proceed?", options: [{ number: 1, label: "Yes" }, { number: 2, label: "No" }] } as never;
    expect(requestsOf(input({ workers: [worker("q", { status: "blocked", prompt })] }))[0]).toMatchObject({ kind: "prompt", title: "/etc/hostname" });
  });
});

test("a quiet Work under Running ends in its state in words, never a date (SPEC 1.2, §11 Tray row end)", () => {
  const m = trayModel(input({ openWork: [
    { work_id: "q", title: "Docs", status: "open", updated_at: "2026-11-11T09:00:00Z" },
    { work_id: "w", title: "Deploy", status: "waiting", wait_for: "the deploy window", updated_at: "2026-11-11T09:00:00Z" },
  ] }));
  expect(m.running.find((r) => r.key === "work:q")?.end).toBe("Queued");
  expect(m.running.find((r) => r.key === "work:w")?.end).toBe("Waiting");
  expect(m.running.every((r) => r.live || r.end === "Queued" || r.end === "Waiting")).toBe(true);
});

test("the tray's headline counts what works, not the quiet Work under Running", () => {
  const m = trayModel(input({ workers: [worker("a")], openWork: [{ work_id: "q", title: "Docs", status: "open" }] }));
  expect(m.title).toBe("1 running");
});
