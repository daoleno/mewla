import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { getOrCreateLocalDeviceIdentity } from "./auth";
import type { StoredServer } from "./storedServerContract";
import { FakeSocket, installMemoryPlatform } from "./testing/memoryPlatform";
import { BIG_FRAME_BYTES, setFrameBatcher, Transport, transportTiming } from "./transport";
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
afterEach(() => {
  Object.assign(transportTiming, saved);
  setFrameBatcher(null);
});

async function connected(): Promise<{ transport: Transport; socket: FakeSocket }> {
  const transport = new Transport(server);
  transport.connect();
  await wait();
  const socket = sockets.at(-1)!;
  socket.open();
  // With a batcher, "connected" reaches the listeners on the next tick, behind the frames before it.
  await wait(1);
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

  test("a big message the computer announced is not silence while it can still be on its way", async () => {
    Object.assign(transportTiming, { slowLinkBytesPerSecond: 1_000 });
    const { transport, socket } = await connected();
    const seen: string[] = [];
    transport.on("frame_coming", (frame) => seen.push(frame.type));
    // 100 bytes at 1 KB/s: 100 ms beyond the 50 ms limit.
    socket.receive({ type: "frame_coming", bytes: 100 });
    await wait(110);
    expect(sockets).toHaveLength(1);
    await wait(120);
    expect(sockets.length).toBeGreaterThan(1);
    // The notice is the transport's own: no listener gets it.
    expect(seen).toEqual([]);
    transport.close();
  });

  test("the announced message's arrival ends its allowance: silence after it counts again", async () => {
    Object.assign(transportTiming, { slowLinkBytesPerSecond: 1_000 });
    const { transport, socket } = await connected();
    socket.receive({ type: "frame_coming", bytes: 10_000_000 });
    socket.receive({ type: "git_diff_status", request_id: "r1" });
    await wait(90);
    expect(sockets.length).toBeGreaterThan(1);
    transport.close();
  });

  test("with a batcher, the frames of a turn reach the listeners together, in order, on the next tick", async () => {
    const batches: number[] = [];
    let inBatch = 0;
    setFrameBatcher((deliver) => {
      inBatch = 0;
      deliver();
      batches.push(inBatch);
    });
    const { transport, socket } = await connected();
    batches.length = 0;
    const seen: number[] = [];
    transport.on("worker_session_updated", (frame) => {
      seen.push(frame.seq as number);
      inBatch++;
    });
    for (let seq = 1; seq <= 5; seq++) socket.receive({ type: "worker_session_updated", seq });
    expect(seen).toEqual([]);
    await wait();
    expect(seen).toEqual([1, 2, 3, 4, 5]);
    expect(batches).toEqual([5]);
    transport.close();
  });

  test("with a batcher, a big frame is a call of its own: small frames go together up to it, and those after it after", async () => {
    const batches: string[][] = [];
    let current: string[] = [];
    setFrameBatcher((deliver) => {
      current = [];
      deliver();
      batches.push(current);
    });
    const { transport, socket } = await connected();
    batches.length = 0;
    const note = (frame: Record<string, unknown>) => current.push(String(frame.seq));
    transport.on("worker_session_updated", note);
    transport.on("codex_conversation_delta", note);
    const page = "x".repeat(BIG_FRAME_BYTES);
    socket.receive({ type: "worker_session_updated", seq: 1 });
    socket.receive({ type: "worker_session_updated", seq: 2 });
    socket.receive({ type: "codex_conversation_delta", seq: 3, page });
    socket.receive({ type: "codex_conversation_delta", seq: 4, page });
    socket.receive({ type: "worker_session_updated", seq: 5 });
    await wait(20);
    expect(batches).toEqual([["1", "2"], ["3"], ["4"], ["5"]]);
    transport.close();
  });

  test("with a batcher, what came in before a drop is handed over before the drop", async () => {
    setFrameBatcher((deliver) => deliver());
    const { transport, socket } = await connected();
    const seen: string[] = [];
    transport.on("worker_session_updated", () => seen.push("update"));
    transport.on("disconnected", () => seen.push("disconnected"));
    socket.receive({ type: "worker_session_updated", seq: 1 });
    socket.close(1006, "gone");
    await wait();
    expect(seen).toEqual(["update", "disconnected"]);
    transport.close();
  });

  // Owner 11-15: back in Mewla after a while in another app, nothing loaded until they switched computers; the
  // computer saw no socket from the phone for 9 min. NSH stops a call into JS past 2 s and runs no catch or finally
  // then, so what the Transport does next must be armed before any listener runs: a plain throw stands in for it here.
  describe("a listener cut off never costs the reconnect", () => {
    test("a disconnected listener that throws: it connects again", async () => {
      const { transport, socket } = await connected();
      transport.on("disconnected", () => {
        throw new Error("interrupted");
      });
      socket.close(1006, "gone");
      await wait(30);
      expect(sockets).toHaveLength(2);
      sockets[1]!.open();
      expect(transport.state).toBe("connected");
      transport.close();
    });

    test("the retry is armed before the frames and the disconnected listeners hear of the end", async () => {
      setFrameBatcher((deliver) => deliver());
      const { transport, socket } = await connected();
      const seen: string[] = [];
      transport.on("worker_session_updated", () => seen.push(transport.state));
      transport.on("disconnected", () => seen.push(transport.state));
      socket.receive({ type: "worker_session_updated", seq: 1 });
      socket.close(1006, "gone");
      await wait();
      expect(seen).toEqual(["waiting", "waiting"]);
      transport.close();
    });

    test("a batch whose listener throws costs only itself: the end and the next connection still arrive, in order", async () => {
      setFrameBatcher((deliver) => deliver());
      const { transport, socket } = await connected();
      const seen: string[] = [];
      transport.on("codex_conversation_delta", () => {
        seen.push("delta");
        throw new Error("interrupted");
      });
      transport.on("disconnected", () => seen.push("disconnected"));
      transport.on("connected", () => seen.push("connected"));
      socket.receive({ type: "codex_conversation_delta", page: "x".repeat(BIG_FRAME_BYTES) });
      socket.close(1006, "gone");
      await wait(30);
      sockets[1]!.open();
      await wait();
      expect(seen).toEqual(["delta", "disconnected", "connected"]);
      transport.close();
    });

    test("the attempt is under way before the state listeners hear connecting", async () => {
      let resolving = 0;
      const transport = new Transport(server, async (stored) => {
        resolving++;
        return stored.url;
      });
      const heard: number[] = [];
      transport.onState((state) => state === "connecting" && heard.push(resolving));
      transport.connect();
      expect(heard).toEqual([1]);
      transport.close();
    });

    test("a state listener that throws on waiting: the retry still comes", async () => {
      const { transport, socket } = await connected();
      transport.onState((state) => {
        if (state === "waiting") throw new Error("interrupted");
      });
      socket.close(1006, "gone");
      await wait(30);
      expect(sockets).toHaveLength(2);
      transport.close();
    });
  });

  describe("an attempt that never lands is made again", () => {
    test("a socket that never opens is closed after the connect limit and dialed again", async () => {
      transportTiming.connectLimitMs = 30;
      const transport = new Transport(server);
      transport.connect();
      await wait(80);
      expect(sockets.length).toBeGreaterThanOrEqual(2);
      expect(sockets[0]!.readyState).toBe(3);
      transport.close();
    });

    test("an address that never resolves is given up after the connect limit", async () => {
      transportTiming.connectLimitMs = 30;
      let calls = 0;
      const transport = new Transport(server, (stored) => (++calls === 1 ? new Promise<string>(() => {}) : Promise.resolve(stored.url)));
      transport.connect();
      await wait(80);
      expect(calls).toBeGreaterThanOrEqual(2);
      expect(sockets.length).toBeGreaterThanOrEqual(1);
      sockets.at(-1)!.open();
      expect(transport.state).toBe("connected");
      transport.close();
    });
  });

  describe("back in front", () => {
    test("a socket silent past what the computer keeps is replaced at once", async () => {
      Object.assign(transportTiming, { silentLimitMs: 60_000, checkMs: 60_000, peerGivesUpMs: 20, firstRetryMs: 60_000 });
      const { transport, socket } = await connected();
      await wait(40);
      transport.resume();
      await wait();
      expect(socket.readyState).toBe(3);
      expect(sockets).toHaveLength(2);
      transport.close();
    });

    test("a socket that carried frames lately is kept", async () => {
      Object.assign(transportTiming, { silentLimitMs: 60_000, checkMs: 60_000, peerGivesUpMs: 1_000 });
      const { transport } = await connected();
      transport.resume();
      await wait();
      expect(sockets).toHaveLength(1);
      expect(transport.state).toBe("connected");
      transport.close();
    });

    test("a state nothing backs (connected with no socket, as a cut-off call left it) connects again", async () => {
      Object.assign(transportTiming, { firstRetryMs: 60_000 });
      const { transport } = await connected();
      // What an end cut off halfway left before: the socket gone, the state still connected, no retry armed.
      (transport as unknown as { socket: unknown }).socket = null;
      transport.resume();
      await wait();
      expect(sockets).toHaveLength(2);
      transport.close();
    });

    test("drop also moves a state nothing backs on (the outbox drops a connection whose send timed out)", async () => {
      Object.assign(transportTiming, { firstRetryMs: 60_000 });
      const { transport } = await connected();
      (transport as unknown as { socket: unknown }).socket = null;
      transport.drop("send timed out");
      await wait();
      expect(sockets).toHaveLength(2);
      transport.close();
    });
  });
});
