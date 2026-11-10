import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { FakeSocket, installMemoryPlatform } from "../testing/memoryPlatform";
import { Transport, transportTiming } from "../transport";
import { WorkerSessions } from "./store";

const server = { id: "s1", name: "Desk", url: "ws://127.0.0.1:9/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const wait = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));
const saved = { ...transportTiming };
let sockets: FakeSocket[];

beforeEach(() => {
  sockets = [];
  installMemoryPlatform({ openSocket: (url, protocols) => { const s = new FakeSocket(url, protocols); sockets.push(s); return s; } });
});
afterEach(() => Object.assign(transportTiming, saved));

const worker = (id: string, extra: Record<string, unknown> = {}) => ({ id, name: id, status: "running", cwd: "/w", ...extra });

async function setup() {
  const transport = new Transport(server);
  let changes = 0;
  const sessions = new WorkerSessions(transport, server, () => changes++);
  transport.connect();
  await wait();
  const socket = sockets.at(-1)!;
  socket.open();
  await wait();
  return { transport, sessions, socket, changes: () => changes };
}

describe("the computer's Sessions", () => {
  test("the list on connect, then numbered changes in order", async () => {
    const { sessions, socket } = await setup();
    expect(sessions.hydrated).toBe(false);
    socket.receive({ type: "worker_session_list", seq: 4, worker_sessions: [worker("a"), worker("b")] });
    expect(sessions.hydrated).toBe(true);
    expect(sessions.workers.map((w) => w.id)).toEqual(["a", "b"]);
    socket.receive({ type: "worker_session_updated", seq: 5, worker_session: worker("a", { status: "blocked" }) });
    socket.receive({ type: "worker_session_created", seq: 6, worker_session: worker("c") });
    socket.receive({ type: "worker_session_archived", seq: 7, worker_session: { id: "b" } });
    expect(sessions.workers.map((w) => `${w.id}:${w.status}`)).toEqual(["a:blocked", "c:running"]);
    // A duplicate changes nothing.
    socket.receive({ type: "worker_session_archived", seq: 7, worker_session: { id: "a" } });
    expect(sessions.find("a")).toBeDefined();
  });

  test("a lost change asks for the list once; the list puts it right", async () => {
    const { sessions, socket } = await setup();
    socket.receive({ type: "worker_session_list", seq: 1, worker_sessions: [worker("a")] });
    const sentBefore = socket.sent.length;
    socket.receive({ type: "worker_session_created", seq: 3, worker_session: worker("c") });
    socket.receive({ type: "worker_session_heartbeat", seq: 4 });
    const asked = socket.sent.slice(sentBefore).map((s) => JSON.parse(s).type);
    expect(asked).toEqual(["list_worker_sessions"]);
    expect(sessions.find("c")).toBeUndefined();
    socket.receive({ type: "worker_session_list", seq: 4, worker_sessions: [worker("a"), worker("b"), worker("c")] });
    expect(sessions.workers.map((w) => w.id)).toEqual(["a", "b", "c"]);
  });

  test("rows stay while offline; a new connection starts a new feed", async () => {
    const { sessions, socket } = await setup();
    socket.receive({ type: "worker_session_list", seq: 9, worker_sessions: [worker("a")] });
    expect(sessions.connection).toBe("connected");
    socket.close();
    await wait();
    expect(sessions.connection).not.toBe("connected");
    expect(sessions.workers.map((w) => w.id)).toEqual(["a"]);
  });

  test("after stop nothing changes it", async () => {
    const { sessions, socket, changes } = await setup();
    sessions.stop();
    const before = changes();
    socket.receive({ type: "worker_session_list", seq: 1, worker_sessions: [worker("a")] });
    expect(changes()).toBe(before);
    expect(sessions.workers).toEqual([]);
  });
});
