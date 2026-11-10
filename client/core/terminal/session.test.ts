import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { FakeSocket, installMemoryPlatform } from "../testing/memoryPlatform";
import { Transport, transportTiming } from "../transport";
import { TerminalSession, type TerminalHandlers } from "./session";

const server = { id: "s1", name: "Desk", url: "ws://127.0.0.1:9/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const wait = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));
/** Waits until `ready()` holds (a timer-driven step finished), at most a second: a fixed sleep loses under load. */
const until = async (ready: () => boolean, ms = 1000) => {
  const end = Date.now() + ms;
  while (!ready() && Date.now() < end) await wait(2);
};
const saved = { ...transportTiming };
let sockets: FakeSocket[];

beforeEach(() => {
  sockets = [];
  installMemoryPlatform({ openSocket: (url, protocols) => { const s = new FakeSocket(url, protocols); sockets.push(s); return s; } });
  Object.assign(transportTiming, { firstRetryMs: 10, maxRetryMs: 40, checkMs: 10, silentLimitMs: 500 });
});
afterEach(() => Object.assign(transportTiming, saved));

async function connected() {
  const transport = new Transport(server);
  transport.connect();
  await wait();
  sockets.at(-1)!.open();
  return transport;
}
const frames = (socket: FakeSocket, type: string) => socket.sent.map((t) => JSON.parse(t)).filter((f) => f.type === type);

function recorder() {
  const log: string[] = [];
  const handlers: TerminalHandlers = {
    reset: () => log.push("reset"),
    output: (data) => log.push(`out:${data}`),
    scrollState: (s) => log.push(`scroll:${s.atBottom}`),
    exit: (code) => log.push(`exit:${code}`),
    error: (m) => log.push(`error:${m}`),
  };
  return { log, handlers };
}

describe("TerminalSession", () => {
  test("opens once the view has a size, then carries output and input of its own attachment only", async () => {
    const transport = await connected();
    const socket = sockets.at(-1)!;
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

  test("two terminals open one after the other, so neither takes the other's attachment", async () => {
    const transport = await connected();
    const socket = sockets.at(-1)!;
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
    sockets.at(-1)!.receive({ type: "terminal_opened", session_id: "t1", cols: 80, rows: 24 });
    sockets.at(-1)!.close(1006, "gone");
    await wait(30);
    sockets.at(-1)!.open();
    await wait();
    const next = sockets.at(-1)!;
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
    sockets.at(-1)!.receive({ type: "terminal_error", code: "open_failed", message: "no such pane" });
    expect(log).toEqual(["error:no such pane"]);
    term.close();
    transport.close();
  });
});
