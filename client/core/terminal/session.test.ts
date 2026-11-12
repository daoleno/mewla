import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { FakeSocket, installMemoryPlatform } from "../testing/memoryPlatform";
import { Transport, transportTiming } from "../transport";
import { historyTiming, HISTORY_FAILED, HISTORY_LOADING, type HistoryRender } from "./history";
import { TerminalSession, type TerminalHandlers } from "./session";

const server = { id: "s1", name: "Desk", url: "ws://127.0.0.1:9/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const wait = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));
/** Waits until `ready()` holds (a timer-driven step finished), at most 4 s (inside bun's 5 s): a fixed sleep, or a short limit, loses under load. */
const until = async (ready: () => boolean, ms = 4000) => {
  const end = Date.now() + ms;
  while (!ready() && Date.now() < end) await wait(2);
};
const saved = { ...transportTiming };
const savedHistory = { ...historyTiming };
let sockets: FakeSocket[];

beforeEach(() => {
  sockets = [];
  installMemoryPlatform({ openSocket: (url, protocols) => { const s = new FakeSocket(url, protocols); sockets.push(s); return s; } });
  // Quick retries; the silence watchdog (transport.test's to prove) never fires here, as a busy runner's late timers would read as silence.
  Object.assign(transportTiming, { firstRetryMs: 10, maxRetryMs: 40, checkMs: 10, silentLimitMs: 60_000 });
  Object.assign(historyTiming, { deltaMs: 5, snapshotMs: 10, timeoutMs: 200 });
});
afterEach(() => {
  Object.assign(transportTiming, saved);
  Object.assign(historyTiming, savedHistory);
});

async function connected() {
  const before = sockets.length;
  const transport = new Transport(server);
  transport.connect();
  await until(() => sockets.length > before);
  // Its own socket; another file's leftover transport (bun runs them in one process) may add one too.
  for (const s of sockets.slice(before)) if (s.readyState === 0) s.open();
  await until(() => transport.state === "connected");
  return transport;
}
/** The socket the transport uses now: another file's leftover transport may have made a later one in `sockets`. */
const own = (transport: Transport) => (transport as unknown as { socket: FakeSocket | null }).socket!;
const frames = (socket: FakeSocket, type: string) => socket.sent.map((t) => JSON.parse(t)).filter((f) => f.type === type);

function recorder() {
  const log: string[] = [];
  const history: HistoryRender[] = [];
  const notices: string[] = [];
  const handlers: TerminalHandlers = {
    reset: () => log.push("reset"),
    output: (data) => log.push(`out:${data}`),
    sessionSize: (cols, rows) => log.push(`size:${cols}x${rows}`),
    scrollState: (s) => log.push(`scroll:${s.atBottom}:${s.inCopyMode}`),
    history: (render) => history.push(render),
    historyNotice: (text) => notices.push(text),
    exit: (code) => log.push(`exit:${code}`),
    error: (m) => log.push(`error:${m}`),
  };
  return { log, history, notices, handlers };
}

/** A terminal attached as `t1` at 80×24 on a fresh connection. */
async function attachedTerminal() {
  const transport = await connected();
  const socket = own(transport);
  const rec = recorder();
  const term = new TerminalSession(transport, "w1", rec.handlers);
  term.resize(80, 24);
  await until(() => frames(socket, "terminal_open").length > 0);
  socket.receive({ type: "terminal_opened", session_id: "t1", cols: 80, rows: 24 });
  return { transport, socket, term, ...rec };
}

const historyRequests = (socket: FakeSocket) => frames(socket, "terminal_history");
const delta = (patch: Record<string, unknown>) => ({
  pane_id: "mh-1", cols: 80, rows: 24, alternate: false, mouse: false, epoch: 7, reset: false, first: 0, from: 0, next: 0, lines: [], ...patch,
});

describe("TerminalSession", () => {
  test("opens once the view has a size, then carries output and input of its own attachment only", async () => {
    const transport = await connected();
    const socket = own(transport);
    const { log, handlers } = recorder();
    const term = new TerminalSession(transport, "w1", handlers);
    expect(frames(socket, "terminal_open")).toHaveLength(0);
    term.resize(80, 24);
    await wait();
    expect(frames(socket, "terminal_open")[0]).toMatchObject({ target_id: "w1", backend: "tmux", cols: 80, rows: 24 });
    socket.receive({ type: "terminal_opened", session_id: "t1", cols: 80, rows: 24, backend: "tmux" });
    socket.receive({ type: "terminal_output", session_id: "t1", data: "hello" });
    socket.receive({ type: "terminal_output", session_id: "other", data: "nope" });
    expect(log).toEqual(["reset", "out:hello"]);
    term.input("ls\r");
    term.resize(100, 30);
    expect(frames(socket, "terminal_input").map((f) => f.data)).toEqual(["\u001b[I", "ls\r"]);
    expect(frames(socket, "terminal_resize").at(-1)).toMatchObject({ session_id: "t1", cols: 100, rows: 30 });
    term.close();
    expect(frames(socket, "terminal_close")).toEqual([{ type: "terminal_close", session_id: "t1" }]);
    transport.close();
  });

  test("shown again, the terminal claims its grid as the Session's size (unchanged) without asking for the history again", async () => {
    const { transport, socket, term } = await attachedTerminal();
    await until(() => historyRequests(socket).length > 0);
    await wait(30);
    const asked = historyRequests(socket).length;
    term.claim();
    expect(frames(socket, "terminal_resize")).toEqual([{ type: "terminal_resize", session_id: "t1", cols: 80, rows: 24 }]);
    await wait(30);
    expect(historyRequests(socket)).toHaveLength(asked);
    term.close();
    term.claim();
    expect(frames(socket, "terminal_resize")).toHaveLength(1);
    transport.close();
  });

  test("the Session's size the computer tells reaches the view before the replay at it, for its own attachment only", async () => {
    const { transport, socket, term, log } = await attachedTerminal();
    await until(() => historyRequests(socket).length > 0);
    await wait(30);
    const asked = historyRequests(socket).length;
    // Another viewer took the size: the view hears it, then the full redraw; its own grid is not sent back.
    socket.receive({ type: "terminal_size", session_id: "t1", cols: 160, rows: 45 });
    socket.receive({ type: "terminal_output", session_id: "t1", data: "\u001bcwide" });
    socket.receive({ type: "terminal_size", session_id: "other", cols: 20, rows: 5 });
    expect(log).toEqual(["reset", "size:160x45", "out:\u001bcwide"]);
    expect(frames(socket, "terminal_resize")).toHaveLength(0);
    // Its history reflowed at the other width: asked for again.
    await until(() => historyRequests(socket).length > asked);
    expect(historyRequests(socket).length).toBeGreaterThan(asked);
    term.close();
    transport.close();
  });

  test("only a tmux terminal gets the FocusIn; a session host terminal would show it as text", async () => {
    const transport = await connected();
    const socket = own(transport);
    const term = new TerminalSession(transport, "w1", recorder().handlers);
    term.resize(80, 24);
    await until(() => frames(socket, "terminal_open").length > 0);
    socket.receive({ type: "terminal_opened", session_id: "h1", cols: 80, rows: 24, backend: "host" });
    term.input("ls\r");
    expect(frames(socket, "terminal_input").map((f) => f.data)).toEqual(["ls\r"]);
    term.close();
    transport.close();
  });

  test("two terminals open one after the other, so neither takes the other's attachment", async () => {
    const transport = await connected();
    const socket = own(transport);
    const a = recorder(), b = recorder();
    const ta = new TerminalSession(transport, "w1", a.handlers);
    const tb = new TerminalSession(transport, "w2", b.handlers);
    ta.resize(80, 24);
    tb.resize(80, 24);
    await until(() => frames(socket, "terminal_open").length > 0);
    await wait();
    expect(frames(socket, "terminal_open").map((f) => f.target_id)).toEqual(["w1"]);
    socket.receive({ type: "terminal_opened", session_id: "t1", cols: 80, rows: 24 });
    await until(() => frames(socket, "terminal_open").length > 1);
    expect(frames(socket, "terminal_open").map((f) => f.target_id)).toEqual(["w1", "w2"]);
    socket.receive({ type: "terminal_opened", session_id: "t2", cols: 80, rows: 24 });
    socket.receive({ type: "terminal_output", session_id: "t2", data: "b" });
    expect(a.log).toEqual(["reset"]);
    expect(b.log).toEqual(["reset", "out:b"]);
    ta.close(); tb.close();
    transport.close();
  });

  test("attaches again after a reconnect; an exit stays exited", async () => {
    const transport = await connected();
    const { log, handlers } = recorder();
    const term = new TerminalSession(transport, "w1", handlers);
    term.resize(80, 24);
    await wait();
    const first = own(transport);
    first.receive({ type: "terminal_opened", session_id: "t1", cols: 80, rows: 24 });
    first.close(1006, "gone");
    await until(() => !!own(transport) && own(transport) !== first);
    const next = own(transport);
    next.open();
    await until(() => frames(next, "terminal_open").length > 0);
    expect(frames(next, "terminal_open")).toHaveLength(1);
    next.receive({ type: "terminal_opened", session_id: "t9", cols: 80, rows: 24 });
    next.receive({ type: "terminal_exit", session_id: "t9", exit_code: 0 });
    expect(log).toEqual(["reset", "reset", "exit:0"]);
    term.resize(90, 24);
    await wait();
    expect(frames(next, "terminal_open")).toHaveLength(1);
    term.close();
    transport.close();
  });

  test("an open the computer refuses says why", async () => {
    const transport = await connected();
    const { log, handlers } = recorder();
    const term = new TerminalSession(transport, "gone", handlers);
    term.resize(80, 24);
    await wait();
    own(transport).receive({ type: "terminal_error", code: "open_failed", message: "no such pane" });
    expect(log).toEqual(["error:no such pane"]);
    term.close();
    transport.close();
  });

  test("tmux copy mode reports its scroll state for its own attachment only", async () => {
    const { transport, socket, term, log } = await attachedTerminal();
    socket.receive({ type: "terminal_scroll_state", session_id: "t1", at_bottom: false, in_copy_mode: true, scroll_position: 12 });
    socket.receive({ type: "terminal_scroll_state", session_id: "other", at_bottom: true, in_copy_mode: false });
    term.scrollToBottom();
    expect(log).toEqual(["reset", "scroll:false:true"]);
    expect(frames(socket, "terminal_scroll_cancel")).toEqual([{ type: "terminal_scroll_cancel", session_id: "t1" }]);
    term.close();
    transport.close();
  });
});

describe("TerminalSession history", () => {
  test("asks for the history once attached, always with a cursor; a tmux snapshot fills the rows", async () => {
    const { transport, socket, term, history, notices } = await attachedTerminal();
    expect(history).toEqual([{ reset: true, removed: 0, rows: [], cols: 0, alternate: false }]);
    expect(notices).toEqual([HISTORY_LOADING]);
    await until(() => historyRequests(socket).length > 0);
    const asked = historyRequests(socket)[0];
    expect(asked).toMatchObject({ session_id: "t1", since: { epoch: 0, next: 0 } });
    socket.receive({ type: "terminal_history", request_id: asked.request_id, session_id: "t1",
      history: { pane_id: "%1", cols: 80, total: 2, alternate: false, ansi: "\u001b[1mone\u001b[0m\ntwo\n" } });
    await until(() => history.length > 1);
    expect(history[1]).toEqual({ reset: true, removed: 0, rows: ["\u001b[1mone\u001b[0m", "two"], cols: 80, alternate: false });
    expect(term.history.text).toEqual(["one", "two"]);
    expect(notices.at(-1)).toBe("");
    term.close();
    transport.close();
  });

  test("output brings the next snapshot: only the rows after what is held, the rows that left dropped from the front", async () => {
    const { transport, socket, term, history } = await attachedTerminal();
    await until(() => historyRequests(socket).length > 0);
    const first = historyRequests(socket)[0];
    socket.receive({ type: "terminal_history", request_id: first.request_id, session_id: "t1", history: { pane_id: "%1", cols: 80, total: 3, alternate: false, ansi: "a\nb\nc\n" } });
    await until(() => history.length > 1);
    socket.receive({ type: "terminal_output", session_id: "t1", data: "d\r\n" });
    await until(() => historyRequests(socket).length > 1);
    const second = historyRequests(socket)[1];
    socket.receive({ type: "terminal_history", request_id: second.request_id, session_id: "t1", history: { pane_id: "%1", cols: 80, total: 3, alternate: false, ansi: "b\nc\nd\n" } });
    await until(() => history.length > 2);
    expect(history[2]).toEqual({ reset: false, removed: 1, rows: ["d"], cols: 80, alternate: false });
    expect(term.history.text).toEqual(["b", "c", "d"]);
    term.close();
    transport.close();
  });

  test("a session host Session answers with numbered deltas; the next request names where the view stands", async () => {
    const { transport, socket, term, history } = await attachedTerminal();
    await until(() => historyRequests(socket).length > 0);
    socket.receive({ type: "terminal_history", request_id: historyRequests(socket)[0].request_id, session_id: "t1",
      history: delta({ reset: true, first: 0, from: 0, next: 2, lines: ["$ ls", "a.txt"], flags: [4, 0] }) });
    await until(() => history.length > 1);
    expect(history[1]).toEqual({ reset: true, removed: 0, rows: ["$ ls", "a.txt"], cols: 80, alternate: false });
    expect(term.history.flags).toEqual([4, 0]);
    socket.receive({ type: "terminal_output", session_id: "t1", data: "x" });
    await until(() => historyRequests(socket).length > 1);
    const next = historyRequests(socket)[1];
    expect(next.since).toEqual({ epoch: 7, next: 2 });
    socket.receive({ type: "terminal_history", request_id: next.request_id, session_id: "t1", history: delta({ first: 1, from: 2, next: 3, lines: ["b.txt"] }) });
    await until(() => history.length > 2);
    expect(history[2]).toEqual({ reset: false, removed: 1, rows: ["b.txt"], cols: 80, alternate: false });
    expect(term.history.text).toEqual(["a.txt", "b.txt"]);
    term.close();
    transport.close();
  });

  test("a delta that does not follow what is held voids it and asks again from nothing", async () => {
    const { transport, socket, term, history } = await attachedTerminal();
    await until(() => historyRequests(socket).length > 0);
    socket.receive({ type: "terminal_history", request_id: historyRequests(socket)[0].request_id, session_id: "t1", history: delta({ reset: true, next: 1, lines: ["a"] }) });
    await until(() => history.length > 1);
    socket.receive({ type: "terminal_output", session_id: "t1", data: "x" });
    await until(() => historyRequests(socket).length > 1);
    socket.receive({ type: "terminal_history", request_id: historyRequests(socket)[1].request_id, session_id: "t1", history: delta({ from: 5, next: 6, lines: ["f"] }) });
    await until(() => historyRequests(socket).length > 2);
    expect(history[2]).toEqual({ reset: true, removed: 0, rows: [], cols: 0, alternate: false });
    expect(historyRequests(socket)[2].since).toEqual({ epoch: 0, next: 0 });
    term.close();
    transport.close();
  });

  test("a program on the alternate screen from the start: the host's empty answer at epoch 0 holds, and the view is not voided again", async () => {
    const { transport, socket, term, history } = await attachedTerminal();
    await until(() => historyRequests(socket).length > 0);
    const empty = delta({ epoch: 0, alternate: true });
    socket.receive({ type: "terminal_history", request_id: historyRequests(socket)[0].request_id, session_id: "t1", history: empty });
    await until(() => history.length > 1);
    expect(history[1]).toEqual({ reset: false, removed: 0, rows: [], cols: 80, alternate: true });
    socket.receive({ type: "terminal_output", session_id: "t1", data: "x" });
    await until(() => historyRequests(socket).length > 1);
    expect(historyRequests(socket)[1].since).toEqual({ epoch: 0, next: 0 });
    socket.receive({ type: "terminal_history", request_id: historyRequests(socket)[1].request_id, session_id: "t1", history: empty });
    await wait(historyTiming.snapshotMs * 4);
    // No void and no request of its own: only output asks again.
    expect(history.length).toBe(2);
    expect(historyRequests(socket).length).toBe(2);
    term.close();
    transport.close();
  });

  test("a failed history says so and retries on request; an answer for an old attachment is dropped", async () => {
    const { transport, socket, term, history, notices } = await attachedTerminal();
    await until(() => historyRequests(socket).length > 0);
    socket.receive({ type: "error", request_id: historyRequests(socket)[0].request_id, code: "terminal_history_failed", message: "capture failed" });
    await until(() => notices.at(-1) === HISTORY_FAILED);
    term.retryHistory();
    expect(notices.at(-1)).toBe(HISTORY_LOADING);
    await until(() => historyRequests(socket).length > 1);
    const stale = historyRequests(socket)[1];
    // The daemon restarted the pane: a new attachment before the answer.
    socket.receive({ type: "terminal_error", session_id: "t1", message: "unknown terminal session" });
    await until(() => frames(socket, "terminal_open").length > 1);
    socket.receive({ type: "terminal_opened", session_id: "t2", cols: 80, rows: 24 });
    socket.receive({ type: "terminal_history", request_id: stale.request_id, session_id: "t1", history: { pane_id: "%1", cols: 80, total: 1, alternate: false, ansi: "old\n" } });
    await wait(20);
    expect(history.some((h) => h.rows.includes("old"))).toBe(false);
    await until(() => historyRequests(socket).some((f) => f.session_id === "t2"));
    term.close();
    transport.close();
  });

  test("the alternate screen has no history: the view is told, so a drag goes to the program", async () => {
    const { transport, socket, term, history } = await attachedTerminal();
    await until(() => historyRequests(socket).length > 0);
    socket.receive({ type: "terminal_history", request_id: historyRequests(socket)[0].request_id, session_id: "t1", history: { pane_id: "%1", cols: 80, total: 40, alternate: true, ansi: "" } });
    await until(() => history.length > 1);
    expect(history[1]).toEqual({ reset: true, removed: 0, rows: [], cols: 80, alternate: true });
    term.close();
    transport.close();
  });

  test("an exit keeps the history and asks for no more", async () => {
    const { transport, socket, term, history } = await attachedTerminal();
    await until(() => historyRequests(socket).length > 0);
    socket.receive({ type: "terminal_history", request_id: historyRequests(socket)[0].request_id, session_id: "t1", history: { pane_id: "%1", cols: 80, total: 1, alternate: false, ansi: "kept\n" } });
    await until(() => history.length > 1);
    socket.receive({ type: "terminal_exit", session_id: "t1", exit_code: 0 });
    await wait(30);
    expect(historyRequests(socket)).toHaveLength(1);
    expect(term.history.text).toEqual(["kept"]);
    term.close();
    transport.close();
  });
});
