import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { getOrCreateLocalDeviceIdentity } from "./auth";
import type { StoredServer } from "./storedServerContract";
import { FakeSocket, installMemoryPlatform } from "./testing/memoryPlatform";
import { Transport, transportTiming } from "./transport";
import { AUTH_SUBPROTOCOL_PREFIX } from "./webSocketAuth";

const server = {
  id: "s1",
  name: "Desk",
  url: "ws://127.0.0.1:9/ws",
  daemonId: "a".repeat(64),
  daemonPublicKey: "b".repeat(64),
} as StoredServer;

const wait = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));
let sockets: FakeSocket[];
const saved = { ...transportTiming };

beforeEach(() => {
  sockets = [];
  installMemoryPlatform({
    openSocket: (url, protocols) => {
      const socket = new FakeSocket(url, protocols);
      sockets.push(socket);
      return socket;
    },
  });
  Object.assign(transportTiming, { firstRetryMs: 10, maxRetryMs: 40, checkMs: 10, silentLimitMs: 50 });
});
afterEach(() => Object.assign(transportTiming, saved));

async function connected(): Promise<{ transport: Transport; socket: FakeSocket }> {
  const transport = new Transport(server);
  transport.connect();
  await wait();
  const socket = sockets.at(-1)!;
  socket.open();
  return { transport, socket };
}

describe("Transport", () => {
  test("signs in with the device key as a subprotocol, never in the URL", async () => {
    const { transport, socket } = await connected();
    expect(socket.url).toBe(server.url);
    expect(socket.protocols).toHaveLength(2); // the auth one, then mewla.sessions.tailless (frames.ts)
    expect(socket.protocols[0].startsWith(AUTH_SUBPROTOCOL_PREFIX)).toBe(true);
    const header = Buffer.from(socket.protocols[0].slice(AUTH_SUBPROTOCOL_PREFIX.length), "base64url").toString();
    expect(header).toContain((await getOrCreateLocalDeviceIdentity()).deviceId);
    expect(transport.state).toBe("connected");
    transport.close();
  });

  test("a burst larger than the socket's 8 MiB all goes out, in order", async () => {
    const { transport, socket } = await connected();
    const pad = "x".repeat(1 << 20);
    const replies = Array.from({ length: 40 }, (_, i) => transport.request<{ n: number }>("get_stats", { n: i, pad }, { reply: "stats" }));
    expect(socket.sent.length).toBe(7);
    await wait(30);
    expect(socket.sent.length).toBe(40);
    const sent = socket.sent.map((text) => JSON.parse(text));
    expect(sent.map((frame) => frame.n)).toEqual(Array.from({ length: 40 }, (_, i) => i));
    for (const frame of sent) socket.receive({ type: "stats", request_id: frame.request_id, n: frame.n });
    expect((await Promise.all(replies)).map((reply) => reply.n)).toEqual(sent.map((frame) => frame.n));
    transport.close();
  });

  test("a request settles only on its own reply, and an error reply rejects it", async () => {
    const { transport, socket } = await connected();
    const first = transport.request("list_providers", {}, { reply: "providers" });
    const second = transport.request("list_providers", {}, { reply: "providers" });
    const [a, b] = socket.sent.map((text) => JSON.parse(text).request_id);
    socket.receive({ type: "error", request_id: b, message: "no such computer" });
    socket.receive({ type: "providers", request_id: "someone-else" });
    socket.receive({ type: "providers", request_id: a, ok: true });
    expect(await first).toMatchObject({ ok: true });
    await expect(second).rejects.toThrow("no such computer");
    transport.close();
  });

  test("a dropped connection fails what is waiting and reconnects with backoff", async () => {
    const { transport, socket } = await connected();
    const pending = transport.request("get_stats", {}, { reply: "stats" });
    socket.close(1006, "network lost");
    await expect(pending).rejects.toThrow("not connected");
    expect(transport.state).toBe("waiting");
    expect(transport.problem).toBe("network lost");
    await wait(15);
    expect(sockets).toHaveLength(2);
    sockets[1].close(1006, "still down");
    await wait(12);
    expect(sockets).toHaveLength(2); // second wait is twice as long
    await wait(15);
    expect(sockets).toHaveLength(3);
    sockets[2].open();
    expect(transport.state).toBe("connected");
    transport.close();
  });

  test("close stops for good", async () => {
    const { transport, socket } = await connected();
    transport.close();
    expect(socket.readyState).toBe(3);
    await wait(30);
    expect(sockets).toHaveLength(1);
    expect(transport.state).toBe("idle");
    expect(() => transport.send({ type: "ping" })).toThrow("not connected");
  });

  test("drop replaces a socket that stopped answering, at once and as lost", async () => {
    const { transport, socket } = await connected();
    const pending = transport.request("get_stats", {}, { reply: "stats" });
    transport.drop("no answer for 180 s");
    await expect(pending).rejects.toThrow("not connected");
    expect(socket.readyState).toBe(3);
    expect(transport.problem).toBe("no answer for 180 s");
    await wait(15);
    expect(sockets).toHaveLength(2);
    transport.close();
  });

  test("a socket that carries nothing for the silent limit is replaced", async () => {
    const { transport } = await connected();
    await wait(80);
    expect(sockets.length).toBeGreaterThan(1);
    transport.close();
  });
});
