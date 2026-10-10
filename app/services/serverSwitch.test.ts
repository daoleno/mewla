import { afterAll, expect, mock, test } from "bun:test";

// Switching servers drives the App's stores through the one live connection:
// the old server's intentional disconnect clears every store before the new
// server publishes, so each store holds at most the current server.

mock.module("react-native", () => ({ Platform: { OS: "web" } }));
mock.module("./auth", () => ({
  buildAuthorizationHeader: async () => "test-authorization",
}));
mock.module("./connectionIssue", () => ({
  diagnoseConnectionIssue: async () => null,
}));

class FakeWebSocket {
  static readonly OPEN = 1;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readyState = 0;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }

  send() {}

  receive(message: object) {
    this.onmessage?.({ data: JSON.stringify(message) });
  }

  close() {
    if (this.readyState === FakeWebSocket.CLOSED) return;
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }
}

const originalWebSocket = globalThis.WebSocket;
Object.assign(globalThis, { WebSocket: FakeWebSocket });

const { WebSocketClient } = await import("./websocket");
const { decideDisconnectLifecycle } = await import("./connectionLifecycle");
const { workerReducer, initialWorkerState } = await import("../store/workers");
const { workReducer, initialWorkState } = await import("../store/work");
const { brainReducer, initialBrainState } = await import("../store/brain");
const { calendarReducer, initialCalendarState } = await import(
  "../store/calendar"
);

afterAll(() => {
  Object.assign(globalThis, { WebSocket: originalWebSocket });
});

function serverFixture(id: string) {
  return {
    id,
    name: id,
    url: `ws://${id}.test/ws`,
    daemonId: `daemon-${id}`,
    daemonPublicKey: `key-${id}`,
  };
}

test("switching servers leaves only the new server in every store", async () => {
  const client = new WebSocketClient();
  let workers = initialWorkerState;
  let work = initialWorkState;
  let brain = initialBrainState;
  let calendar = initialCalendarState;

  // The same event-to-store wiring as the App root.
  client.on("connected", (data) => {
    workers = workerReducer(workers, {
      type: "SET_SERVER_CONNECTION_STATE",
      serverId: data.serverId,
      connectionState: "connected",
    });
  });
  client.on("disconnected", (data) => {
    const decision = decideDisconnectLifecycle(data.reason);
    workers = workerReducer(workers, {
      type: "SET_SERVER_CONNECTION_STATE",
      serverId: data.serverId,
      connectionState: decision.connectionState,
    });
    if (!decision.clearServerCaches) return;
    workers = workerReducer(workers, { type: "REMOVE_SERVER", serverId: data.serverId });
    work = workReducer(work, { type: "REMOVE_SERVER", serverId: data.serverId });
    brain = brainReducer(brain, { type: "REMOVE_SERVER", serverId: data.serverId });
    calendar = calendarReducer(calendar, { type: "REMOVE_SERVER", serverId: data.serverId });
  });
  client.on("worker_session_list", (data) => {
    workers = workerReducer(workers, {
      type: "UPSERT_SERVER_WORKERS",
      serverId: data.serverId,
      serverName: data.serverName,
      serverUrl: data.serverUrl,
      workers: data.worker_sessions,
    });
  });
  client.on("work_items_snapshot", (data) => {
    work = workReducer(work, {
      type: "WORK_ITEMS_SNAPSHOT",
      serverId: data.serverId,
      serverName: data.serverName,
      serverUrl: data.serverUrl,
      workItems: data.work_items,
    });
  });
  client.on("brain_snapshot", (data) => {
    brain = brainReducer(brain, {
      type: "BRAIN_SNAPSHOT",
      serverId: data.serverId,
      serverName: data.serverName,
      serverUrl: data.serverUrl,
      brain: data.brain,
    });
  });
  client.on("calendar_items_snapshot", (data) => {
    calendar = calendarReducer(calendar, {
      type: "CALENDAR_SNAPSHOT",
      serverId: data.serverId,
      serverName: data.serverName,
      serverUrl: data.serverUrl,
      items: data.calendar_items,
    });
  });

  const publish = (socket: FakeWebSocket, prefix: string) => {
    socket.receive({
      type: "worker_session_list",
      worker_sessions: [{ id: `${prefix}-worker`, name: "worker", status: "running" }],
    });
    socket.receive({
      type: "work_items_snapshot",
      work_items: [{ id: `${prefix}-work`, project: "p", title: "t", body: "b" }],
    });
    socket.receive({ type: "brain_snapshot", brain: { chat_thread_id: `${prefix}-thread` } });
    socket.receive({
      type: "calendar_items_snapshot",
      calendar_items: [{ id: `${prefix}-cal`, title: "c", revision: 1 }],
    });
  };
  const serverIdsInStores = () => ({
    workers: [...new Set(workers.workers.map((worker) => worker.serverId))],
    connections: Object.keys(workers.serverConnections),
    hydrated: Object.keys(workers.hydratedServers),
    listGenerations: Object.keys(workers.workerSessionListGenerationByServer),
    work: [...new Set(Object.values(work.byKey).map((item) => item.serverId))],
    brain: Object.keys(brain.byServer),
    calendar: Object.keys(calendar.byServer),
  });
  const only = (serverId: string) => ({
    workers: [serverId],
    connections: [serverId],
    hydrated: [serverId],
    listGenerations: [serverId],
    work: [serverId],
    brain: [serverId],
    calendar: [serverId],
  });

  const a = serverFixture("server-a");
  const b = serverFixture("server-b");

  const flush = async () => {
    for (let i = 0; i < 4; i += 1) await Promise.resolve();
  };

  client.connect(a);
  await flush();
  const socketA = FakeWebSocket.instances.at(-1)!;
  socketA.open();
  publish(socketA, "a");
  expect(serverIdsInStores()).toEqual(only(a.id));

  client.connect(b);
  await flush();
  const socketB = FakeWebSocket.instances.at(-1)!;
  expect(socketA.readyState).toBe(FakeWebSocket.CLOSED);
  expect(serverIdsInStores()).toEqual({
    workers: [],
    connections: [],
    hydrated: [],
    listGenerations: [],
    work: [],
    brain: [],
    calendar: [],
  });

  socketB.open();
  // A late frame from the old socket publishes nothing.
  socketA.onmessage?.({
    data: JSON.stringify({ type: "brain_snapshot", brain: { chat_thread_id: "late" } }),
  });
  publish(socketB, "b");
  expect(serverIdsInStores()).toEqual(only(b.id));

  client.connect(a);
  await flush();
  FakeWebSocket.instances.at(-1)!.open();
  expect(serverIdsInStores().workers).toEqual([]);
  expect(Object.keys(workers.serverConnections)).toEqual([a.id]);
  client.disconnect();
});
