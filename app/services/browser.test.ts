import { afterEach, expect, mock, test } from "bun:test";
mock.module("./auth", () => ({ buildAuthorizationHeader: async () => "signed-fixture" }));
mock.module("./pinnedTransport", () => ({ resolveStoredServerURL: async (server: { url: string }) => server.url }));
const { BrowserRequestError, BrowserViewer, browserRequest } = await import("./browser");
class Socket {
  static OPEN = 1;
  static all: Socket[] = [];
  readyState = 1;
  sent: any[] = [];
  onopen?: () => void;
  onmessage?: (event: { data: string }) => void;
  onclose?: () => void;
  onerror?: () => void;
  constructor(public url: string, protocols?: unknown, public options?: unknown) { Socket.all.push(this); }
  send(value: string) { this.sent.push(JSON.parse(value)); }
  close() { this.readyState = 3; }
  response(message: any) { this.onmessage?.({ data: JSON.stringify(message) }); }
}
const original = globalThis.WebSocket;
const viewers: InstanceType<typeof BrowserViewer>[] = [];
afterEach(() => { for (const viewer of viewers.splice(0)) viewer.close(); globalThis.WebSocket = original; Socket.all = []; });
const server = { id: "server-a", daemonId: "fixture", url: "wss://server-a.test/ws", name: "A" };
const lease = { epoch: 1, generation: "process-1", target: "tab-1", kind: "human", expires_at: "later" };
async function setup() {
  globalThis.WebSocket = Socket as any;
  const statuses: string[] = [];
  const frames: any[] = [];
  const viewer = new BrowserViewer(server as any, "resource", { frame: (f) => frames.push(f), status: (s) => statuses.push(s), resource: () => {} });
  viewers.push(viewer);
  await viewer.connect();
  const socket = Socket.all.at(-1)!;
  socket.onopen?.();
  return { viewer, socket, statuses, frames };
}
async function control(viewer: InstanceType<typeof BrowserViewer>, socket: Socket) {
  const promise = viewer.control();
  socket.response({ type: "response", request_id: socket.sent.at(-1).request_id, response: { lease } });
  await promise;
}
test("viewer binds canonical server/resource, ACKs only after renderer and drops late callbacks", async () => {
  const { viewer, socket, frames } = await setup();
  expect(socket.url).toBe("wss://server-a.test/browser/viewer?id=resource");
  expect(socket.options).toEqual({ headers: { Authorization: "signed-fixture" } });
  const frame = { type: "frame", seq: 1, data: "aGVsbG8=", metadata: { deviceWidth: 1, deviceHeight: 1 } };
  socket.response(frame);
  expect(frames).toHaveLength(1);
  expect(socket.sent).toEqual([{ type: "ping" }]);
  viewer.ack(1);
  expect(socket.sent.at(-1)).toEqual({ type: "ack", seq: 1 });
  viewer.close();
  socket.response({ ...frame, seq: 2 });
  expect(frames).toHaveLength(1);
  expect(socket.sent.at(-1)).toEqual({ type: "close" });
});
test("release invalidates queued input and a late command cannot restore control", async () => {
  const { viewer, socket } = await setup();
  await control(viewer, socket);
  const inFlight = viewer.command({ kind: "snapshot" }).catch((e) => e);
  await Promise.resolve();
  const request = socket.sent.at(-1);
  const queued = viewer.command({ kind: "press", text: "Enter" }).catch((e) => e);
  const release = viewer.release();
  const releaseRequest = socket.sent.at(-1);
  socket.response({ type: "response", request_id: request.request_id, response: { lease } });
  socket.response({ type: "response", request_id: releaseRequest.request_id, response: {} });
  await release;
  expect(await inFlight).toBeInstanceOf(Error);
  expect(await queued).toBeInstanceOf(Error);
  await expect(viewer.command({ kind: "snapshot" })).rejects.toThrow("Take control");
  expect(socket.sent.filter((m) => m.command?.kind === "press")).toHaveLength(0);
});
test("disconnect rejects pending input and reconnect never silently takes control", async () => {
  const { viewer, socket } = await setup();
  await control(viewer, socket);
  const pending = viewer.command({ kind: "input", input: { kind: "keyDown", key: "Shift" } }).catch((e) => e);
  await Promise.resolve();
  socket.onclose?.();
  expect(await pending).toBeInstanceOf(Error);
  const next = await setup();
  // Opening only asks for fresh state; it never requests control by itself.
  expect(next.socket.sent).toEqual([{ type: "ping" }]);
  await expect(next.viewer.command({ kind: "snapshot" })).rejects.toThrow("Take control");
});
test("input queue remains bounded", async () => {
  const { viewer, socket } = await setup();
  await control(viewer, socket);
  const waiting = Array.from({ length: 32 }, () => viewer.command({ kind: "snapshot" }).catch((e) => e));
  await expect(viewer.command({ kind: "snapshot" })).rejects.toThrow("Input is busy");
  viewer.close();
  await Promise.all(waiting);
});
test("plain-text server failures become typed errors instead of JSON parse errors", async () => {
  const fetchOriginal = globalThis.fetch;
  globalThis.fetch = (async () => new Response("404 page not found", { status: 404 })) as any;
  try {
    const error = await browserRequest(server as any, { action: "list" }).catch((e) => e);
    expect(error).toBeInstanceOf(BrowserRequestError);
    expect(error.status).toBe(404);
    expect(error.detail).toBe("404 page not found");
    globalThis.fetch = (async () => new Response(JSON.stringify({ error: "Browser not found" }), { status: 409 })) as any;
    await expect(browserRequest(server as any, { action: "start", id: "x" })).rejects.toThrow("Browser not found");
  } finally { globalThis.fetch = fetchOriginal; }
});
test("viewer reports lost liveness on close without reporting after detach", async () => {
  globalThis.WebSocket = Socket as any;
  const states: string[] = [];
  const viewer = new BrowserViewer(server as any, "resource", { frame() {}, status() {}, resource() {}, connection: (s) => states.push(s) });
  viewers.push(viewer);
  await viewer.connect();
  const socket = Socket.all.at(-1)!;
  socket.onopen?.();
  socket.onclose?.();
  expect(states).toEqual(["live", "lost"]);
  viewer.close();
  socket.onclose?.();
  expect(states).toEqual(["live", "lost"]);
});
test("viewer reports lease grant and loss from server responses only", async () => {
  globalThis.WebSocket = Socket as any;
  const leases: boolean[] = [];
  const viewer = new BrowserViewer(server as any, "resource", { frame() {}, status() {}, resource() {}, lease: (held) => leases.push(held) });
  viewers.push(viewer);
  await viewer.connect();
  const socket = Socket.all.at(-1)!;
  socket.onopen?.();
  await control(viewer, socket);
  expect(leases).toEqual([true]);
  const pending = viewer.command({ kind: "snapshot" }).catch((e) => e);
  await Promise.resolve();
  socket.response({ type: "response", request_id: socket.sent.at(-1).request_id, error: "Browser control changed; request control again" });
  expect(await pending).toBeInstanceOf(Error);
  expect(leases).toEqual([true, false]);
});
