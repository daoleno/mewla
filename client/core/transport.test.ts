import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { getOrCreateLocalDeviceIdentity } from "./auth";
import type { StoredServer } from "./storedServerContract";
import { FakeSocket, installMemoryPlatform } from "./testing/memoryPlatform";
import { AWAY_HOLD_BYTES, BIG_FRAME_BYTES, type PathMoves, setFrameBatcher, Transport, transportTiming } from "./transport";
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

/** Until the Transport has opened `count` sockets: a retry signs in on the platform (core/ed25519.ts), a turn after its timer. */
async function socketsReach(count: number): Promise<void> {
  const deadline = Date.now() + 2_000;
  while (sockets.length < count && Date.now() < deadline) await wait(1);
  expect(sockets).toHaveLength(count);
}

async function connected(): Promise<{ transport: Transport; socket: FakeSocket }> {
  const transport = new Transport(server);
  const before = sockets.length;
  transport.connect();
  // The sign-in is signed by the platform (core/ed25519.ts), on a later turn.
  const deadline = Date.now() + 2_000;
  while (sockets.length === before && Date.now() < deadline) await wait(1);
  if (sockets.length === before) throw new Error("the Transport opened no socket within 2 s");
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
    await socketsReach(2);
    sockets[1].close(1006, "still down");
    await wait(12);
    expect(sockets).toHaveLength(2); // second wait is twice as long
    await socketsReach(3);
    sockets[2].open();
    expect(transport.state).toBe("connected");
    transport.close();
  });

  describe("the phone's network changes (the hosts' link.network)", () => {
    test("a wait for the next retry ends at once, not after the backoff", async () => {
      Object.assign(transportTiming, { firstRetryMs: 60_000 });
      const { transport, socket } = await connected();
      socket.close(1006, "network lost");
      expect(transport.state).toBe("waiting");
      transport.networkChanged(true);
      await socketsReach(2);
      sockets[1].open();
      expect(transport.state).toBe("connected");
      transport.close();
    });

    test("an attempt in flight goes on; if it fails, the next follows after the first wait, not the grown backoff", async () => {
      Object.assign(transportTiming, { firstRetryMs: 10, maxRetryMs: 400 });
      const { transport, socket } = await connected();
      socket.close(1006, "network lost");
      for (const count of [2, 3, 4]) {
        await socketsReach(count);
        if (count < 4) sockets[count - 1].close(1006, "unreachable");
      }
      // The backoff is at 80 ms now; the network comes back while the fourth attempt is under way.
      transport.networkChanged(true);
      expect(sockets).toHaveLength(4);
      sockets[3].close(1006, "unreachable");
      await wait(30);
      expect(sockets).toHaveLength(5);
      transport.close();
    });

    test("going offline changes nothing: the ends of the sockets the system took away say so", async () => {
      Object.assign(transportTiming, { firstRetryMs: 60_000 });
      const { transport, socket } = await connected();
      socket.close(1006, "network lost");
      transport.networkChanged(false);
      await wait(20);
      expect(sockets).toHaveLength(1);
      expect(transport.state).toBe("waiting");
      transport.close();
    });

    test("a live socket that answers on the new network is kept", async () => {
      const { transport, socket } = await connected();
      Object.assign(transportTiming, { networkProofMs: 20, silentLimitMs: 10_000 });
      transport.networkChanged(true);
      const ping = JSON.parse(socket.sent.at(-1)!);
      expect(ping.type).toBe("link_ping");
      socket.receive({ type: "link_pong", request_id: ping.request_id });
      await wait(40);
      expect(sockets).toHaveLength(1);
      expect(transport.state).toBe("connected");
      transport.close();
    });

    test("a live socket left on the network that went away is replaced within the proof time, not the silent limit", async () => {
      const { transport, socket } = await connected();
      Object.assign(transportTiming, { networkProofMs: 20, silentLimitMs: 10_000 });
      transport.networkChanged(true);
      await socketsReach(2);
      expect(socket.readyState).toBe(3);
      expect(transport.problem).toBe("network changed");
      transport.close();
    });
  });

  test("the next connection tells the computer how the last one ended, on which path and for how long it was down", async () => {
    const { transport, socket } = await connected();
    await wait(5);
    socket.close(1006, "network lost");
    await socketsReach(2);
    sockets[1].close(1006, "still down");
    await socketsReach(3);
    sockets[2].open();
    const report = sockets[2].sent.map((text) => JSON.parse(text)).find((frame) => frame.type === "client_diagnostic");
    expect(report?.name).toBe("link_drop");
    expect(report?.body).toMatch(/^why=network_lost path=address age=\d+\.\ds down=\d+\.\ds attempts=1 last=still_down away=false network_changes=0$/);
    // Once: the connection after it has nothing to report until one ends again.
    sockets[2].close(1000, "");
    await socketsReach(4);
    sockets[3].open();
    expect(sockets[3].sent.map((text) => JSON.parse(text)).filter((frame) => frame.type === "client_diagnostic")).toHaveLength(1);
    const first = sockets[0].sent.map((text) => JSON.parse(text)).filter((frame) => frame.type === "client_diagnostic");
    expect(first).toHaveLength(0);
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
    await socketsReach(2);
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
      await socketsReach(2);
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
      await socketsReach(2);
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
      await socketsReach(2);
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
      await socketsReach(2);
      transport.close();
    });

    test("drop also moves a state nothing backs on (the outbox drops a connection whose send timed out)", async () => {
      Object.assign(transportTiming, { firstRetryMs: 60_000 });
      const { transport } = await connected();
      (transport as unknown as { socket: unknown }).socket = null;
      transport.drop("send timed out");
      await wait();
      await socketsReach(2);
      transport.close();
    });
  });

  // The system may freeze an app in the background at any moment, and NSH cuts off on thaw the call it froze (the
  // frozen time counts against its 2 s): out of sight, frames wait, but for what has to work there.
  describe("out of sight", () => {
    test("frames wait for the app to be back, then reach every listener in order; away listeners hear them at once, once", async () => {
      const batches: number[] = [];
      setFrameBatcher((deliver) => {
        const before = seen.length;
        deliver();
        batches.push(seen.length - before);
      });
      const seen: string[] = [];
      const { transport, socket } = await connected();
      transport.on("worker_session_updated", (frame) => seen.push(`list ${frame.seq}`));
      transport.on("live_state", (frame) => seen.push(`live ${frame.seq}`), { whileAway: true });
      transport.on("worker_session_updated", (frame) => seen.push(`away ${frame.seq}`), { whileAway: true });
      transport.setAway(true);
      socket.receive({ type: "worker_session_updated", seq: 1 });
      socket.receive({ type: "live_state", seq: 2 });
      socket.receive({ type: "worker_session_updated", seq: 3 });
      await wait(10);
      expect(seen).toEqual(["away 1", "live 2", "away 3"]);
      transport.setAway(false);
      await wait(10);
      expect(seen).toEqual(["away 1", "live 2", "away 3", "list 1", "list 3"]);
      transport.close();
    });

    test("a tick armed just before the app left hands nothing over", async () => {
      setFrameBatcher((deliver) => deliver());
      const { transport, socket } = await connected();
      const seen: number[] = [];
      transport.on("worker_session_updated", (frame) => seen.push(frame.seq as number));
      socket.receive({ type: "worker_session_updated", seq: 1 });
      transport.setAway(true);
      await wait(10);
      expect(seen).toEqual([]);
      transport.setAway(false);
      await wait(10);
      expect(seen).toEqual([1]);
      transport.close();
    });

    test("an answer to the phone's own request is handled at once, ahead of the frames waiting", async () => {
      setFrameBatcher((deliver) => deliver());
      const { transport, socket } = await connected();
      const seen: string[] = [];
      transport.on("worker_session_updated", () => seen.push("update"));
      transport.setAway(true);
      const reply = transport.request<{ n: number }>("get_stats", {}, { reply: "stats" });
      const id = JSON.parse(socket.sent.at(-1)!).request_id;
      socket.receive({ type: "worker_session_updated", seq: 1 });
      socket.receive({ type: "stats", request_id: id, n: 7, pad: "x".repeat(BIG_FRAME_BYTES) });
      expect((await reply).n).toBe(7);
      expect(seen).toEqual([]);
      // What follows under the same id (a subscription's stream) is news: it waits.
      socket.receive({ type: "worker_session_updated", request_id: id, seq: 2 });
      expect(seen).toEqual([]);
      // Someone else's request id is the computer's news: it waits.
      socket.receive({ type: "stats", request_id: "not-mine", n: 8 });
      let others = 0;
      transport.on("stats", () => others++);
      await wait(10);
      expect(others).toBe(0);
      transport.setAway(false);
      await wait(10);
      expect(seen).toEqual(["update", "update"]);
      expect(others).toBe(1);
      transport.close();
    });

    test("a big frame waits unread: even an away listener hears it only back in front, once", async () => {
      setFrameBatcher((deliver) => deliver());
      const { transport, socket } = await connected();
      const heard: number[] = [];
      transport.on("codex_conversation_snapshot", (frame) => heard.push((frame.page as string).length), { whileAway: true });
      transport.setAway(true);
      socket.receive({ type: "codex_conversation_snapshot", page: "x".repeat(BIG_FRAME_BYTES) });
      await wait(10);
      expect(heard).toEqual([]);
      transport.setAway(false);
      await wait(10);
      expect(heard).toEqual([BIG_FRAME_BYTES]);
      transport.close();
    });

    test("a drop out of sight: what waits on the connection hears it at once; the rest back in front, after the frames before it", async () => {
      setFrameBatcher((deliver) => deliver());
      const { transport, socket } = await connected();
      const seen: string[] = [];
      transport.on("worker_session_updated", () => seen.push("update"));
      transport.on("disconnected", () => seen.push("disconnected"));
      transport.setAway(true);
      const pending = transport.request("get_stats", {}, { reply: "stats" });
      socket.receive({ type: "worker_session_updated", seq: 1 });
      socket.close(1006, "gone");
      await expect(pending).rejects.toThrow("not connected");
      expect(seen).toEqual([]);
      transport.setAway(false);
      await wait(10);
      expect(seen).toEqual(["update", "disconnected"]);
      transport.close();
    });

    test("past what may wait, the frames are let go and the app connects again back in front", async () => {
      setFrameBatcher((deliver) => deliver());
      const { transport, socket } = await connected();
      const seen: number[] = [];
      transport.on("codex_conversation_snapshot", (frame) => seen.push(frame.seq as number));
      transport.setAway(true);
      const page = "x".repeat(1024 * 1024);
      for (let seq = 1; seq <= Math.ceil(AWAY_HOLD_BYTES / page.length) + 1; seq++) socket.receive({ type: "codex_conversation_snapshot", seq, page });
      expect(sockets).toHaveLength(1);
      transport.setAway(false);
      await socketsReach(2);
      await wait(10);
      expect(seen).toEqual([]);
      transport.close();
    });

    test("the frames that waited come back a bounded batch a call", async () => {
      const batches: number[] = [];
      let count = 0;
      setFrameBatcher((deliver) => {
        const before = count;
        deliver();
        batches.push(count - before);
      });
      const { transport, socket } = await connected();
      transport.on("worker_session_updated", () => count++);
      batches.length = 0;
      transport.setAway(true);
      const pad = "x".repeat(7 * 1024);
      for (let seq = 1; seq <= 120; seq++) socket.receive({ type: "worker_session_updated", seq, pad });
      transport.setAway(false);
      await wait(30);
      expect(count).toBe(120);
      expect(batches.length).toBeGreaterThan(2);
      expect(Math.max(...batches)).toBeLessThanOrEqual(40);
      transport.close();
    });

    test("without a batcher too, frames wait and come back in order", async () => {
      const { transport, socket } = await connected();
      const seen: number[] = [];
      transport.on("worker_session_updated", (frame) => seen.push(frame.seq as number));
      transport.setAway(true);
      socket.receive({ type: "worker_session_updated", seq: 1 });
      socket.receive({ type: "worker_session_updated", seq: 2 });
      expect(seen).toEqual([]);
      transport.setAway(false);
      expect(seen).toEqual([1, 2]);
      transport.close();
    });
  });
});

describe("a connection on the relay moves onto a direct path (make-before-break)", () => {
  const DIRECT = "ws://127.0.0.1:7/direct";
  /** A computer whose direct path answers each check as `answers` says (the last answer repeats), and where the connection is. */
  function fakeMoves(answers: boolean[]) {
    const record = { checks: 0, moved: 0, abandoned: 0, path: "relay" };
    const moves: PathMoves = {
      current: () => record.path,
      check: async () => {
        const ok = answers[Math.min(record.checks, answers.length - 1)];
        record.checks++;
        return ok ? { url: DIRECT, path: "lan" } : null;
      },
      moved: () => {
        record.moved++;
        record.path = "lan";
      },
      abandoned: () => void record.abandoned++,
    };
    return { moves, record };
  }

  async function until(condition: () => boolean, what: string): Promise<void> {
    const deadline = Date.now() + 2_000;
    while (!condition() && Date.now() < deadline) await wait(1);
    if (!condition()) throw new Error(`never: ${what}`);
  }

  /** Connected on the relay, with what the Transport tells its listeners recorded. */
  async function onRelay(answers: boolean[]) {
    const { moves, record } = fakeMoves(answers);
    const transport = new Transport(server, undefined, moves);
    const states: string[] = [];
    const heard: string[] = [];
    transport.onState((state) => states.push(state));
    for (const type of ["connected", "disconnected", "moved"]) transport.on(type, () => heard.push(type), { whileAway: true });
    transport.connect();
    await socketsReach(1);
    const relay = sockets[0]!;
    relay.open();
    await wait(1);
    return { transport, relay, record, states, heard };
  }

  /** Waits for the move's socket to the direct path. */
  async function moveSocket(): Promise<FakeSocket> {
    await until(() => sockets.length === 2, "a socket to the direct path");
    const direct = sockets[1]!;
    expect(direct.url).toBe(DIRECT);
    // Signed in like any other connection.
    expect(direct.protocols[0]!.startsWith(AUTH_SUBPROTOCOL_PREFIX)).toBe(true);
    return direct;
  }

  function closes(socket: FakeSocket): Array<{ code: number; reason: string }> {
    const seen: Array<{ code: number; reason: string }> = [];
    const close = socket.close.bind(socket);
    socket.close = (code = 1000, reason = "") => {
      seen.push({ code, reason });
      close(code, reason);
    };
    return seen;
  }

  beforeEach(() => {
    Object.assign(transportTiming, { silentLimitMs: 60_000, moveCheckMs: 2, moveCheckMaxMs: 8, moveHealthyChecks: 3, moveOpenLimitMs: 2_000, moveBackoffMs: 60_000, retireLimitMs: 2_000 });
  });

  test("moves without any disconnect: the state stays connected, nothing hears 'disconnected', the old socket closes normally", async () => {
    const { transport, relay, record, states, heard } = await onRelay([true]);
    const relayCloses = closes(relay);
    const direct = await moveSocket();
    // Until the new socket is open the relay carries everything.
    expect(record.checks).toBe(3);
    transport.send({ type: "ping_before" });
    expect(relay.sent.map((text) => JSON.parse(text).type)).toContain("ping_before");
    direct.open();
    await wait(1);
    expect(record.moved).toBe(1);
    expect(states).toEqual(["connecting", "connected"]);
    expect(heard).toEqual(["connected", "moved"]);
    expect(transport.state).toBe("connected");
    // Nothing was owed on the relay: it is closed at once, with a normal close.
    expect(relayCloses).toEqual([{ code: 1000, reason: "moved" }]);
    // The move is reported on the new socket, and everything from now on goes out there.
    const report = direct.sent.map((text) => JSON.parse(text)).find((frame) => frame.name === "link_move");
    expect(report?.body).toMatch(/^from=relay to=lan after=\d+\.\ds owed=0$/);
    transport.send({ type: "ping_after" });
    expect(direct.sent.map((text) => JSON.parse(text).type)).toContain("ping_after");
    expect(relay.sent.map((text) => JSON.parse(text).type)).not.toContain("ping_after");
    // The frames of the new socket reach the listeners.
    const stats: unknown[] = [];
    transport.on("stats", (frame) => stats.push(frame.n));
    direct.receive({ type: "stats", n: 1 });
    expect(stats).toEqual([1]);
    // No more checks once on the direct path.
    const checks = record.checks;
    await wait(20);
    expect(record.checks).toBe(checks);
    transport.close();
  });

  test("a request in flight on the relay is answered there after the move; the relay's other frames are not handed over twice", async () => {
    const { transport, relay, heard } = await onRelay([true]);
    const relayCloses = closes(relay);
    const direct = await moveSocket();
    const answer = transport.request<{ n: number }>("get_stats", {}, { reply: "stats", timeoutMs: 1_000 });
    const asked = JSON.parse(relay.sent.at(-1)!);
    expect(asked.type).toBe("get_stats");
    direct.open();
    await wait(1);
    expect(heard).toEqual(["connected", "moved"]);
    // The relay still owes the answer: it stays open for it.
    expect(relayCloses).toEqual([]);
    const sessions: unknown[] = [];
    transport.on("worker_session_updated", (frame) => sessions.push(frame.seq));
    relay.receive({ type: "worker_session_updated", seq: 9 });
    relay.receive({ type: "stats", request_id: asked.request_id, n: 7 });
    expect((await answer).n).toBe(7);
    expect(sessions).toEqual([]);
    expect(relayCloses).toEqual([{ code: 1000, reason: "moved" }]);
    expect(direct.sent.map((text) => JSON.parse(text).name)).toContain("link_move");
    expect(JSON.parse(direct.sent.find((text) => JSON.parse(text).name === "link_move")!).body).toEndWith("owed=1");
    // The old socket's end changes nothing.
    relay.onclose?.({ code: 1006, reason: "" });
    expect(transport.state).toBe("connected");
    expect(heard).toEqual(["connected", "moved"]);
    transport.close();
  });

  test("a move whose socket fails leaves the relay connection untouched and backs off", async () => {
    const { transport, relay, record, states, heard } = await onRelay([true]);
    const relayCloses = closes(relay);
    const direct = await moveSocket();
    direct.onerror?.({});
    expect(record.abandoned).toBe(1);
    expect(record.moved).toBe(0);
    expect(relayCloses).toEqual([]);
    expect(relay.readyState).toBe(1);
    expect(transport.state).toBe("connected");
    expect(states).toEqual(["connecting", "connected"]);
    expect(heard).toEqual(["connected"]);
    transport.send({ type: "still_on_relay" });
    expect(relay.sent.map((text) => JSON.parse(text).type)).toContain("still_on_relay");
    const stats: unknown[] = [];
    transport.on("stats", (frame) => stats.push(frame.n));
    relay.receive({ type: "stats", n: 3 });
    expect(stats).toEqual([3]);
    // Backed off (a minute here): no check, no new socket meanwhile.
    const checks = record.checks;
    await wait(30);
    expect(record.checks).toBe(checks);
    expect(sockets).toHaveLength(2);
    transport.close();
  });

  test("a move whose socket never opens is given up after its limit; the relay is untouched", async () => {
    transportTiming.moveOpenLimitMs = 10;
    const { transport, relay, record, heard } = await onRelay([true]);
    await moveSocket();
    await until(() => record.abandoned === 1, "the move given up");
    expect(relay.readyState).toBe(1);
    expect(transport.state).toBe("connected");
    expect(heard).toEqual(["connected"]);
    transport.close();
  });

  test("moves only after the direct path answered the checks in a row: a failed check starts the count over", async () => {
    const { transport, record } = await onRelay([true, true, false, true, true, true]);
    await moveSocket();
    expect(record.checks).toBe(6);
    transport.close();
  });

  test("the relay ending during a move is an honest drop: the move is given up and the connection reconnects", async () => {
    const { transport, relay, record, heard } = await onRelay([true]);
    const direct = await moveSocket();
    relay.close(1006, "");
    expect(record.abandoned).toBe(1);
    expect(direct.readyState).toBe(3);
    expect(transport.state).toBe("waiting");
    await wait(1);
    expect(heard).toEqual(["connected", "disconnected"]);
    // The move's socket opening late changes nothing.
    direct.onopen?.({});
    expect(record.moved).toBe(0);
    transport.close();
  });

  test("after the move, the direct path dying is a normal reconnect, and another move waits out the backoff", async () => {
    const { transport, record, heard } = await onRelay([true]);
    const direct = await moveSocket();
    direct.open();
    await wait(1);
    direct.close(1006, "");
    expect(transport.state).toBe("waiting");
    await wait(1);
    expect(heard).toEqual(["connected", "moved", "disconnected"]);
    // The next connection is on the relay again (the path the host chose); it does not check before the backoff.
    record.path = "relay";
    await socketsReach(3);
    sockets[2]!.open();
    await wait(1);
    const checks = record.checks;
    await wait(30);
    expect(record.checks).toBe(checks);
    transport.close();
  });

  test("a direct path that keeps dying soon after each move is moved onto less often each time (no ping-pong)", async () => {
    Object.assign(transportTiming, { moveBackoffMs: 60, moveBackoffMaxMs: 10_000, moveTrustMs: 60_000 });
    const { transport, record } = await onRelay([true]);
    /** Moves, lets the direct socket die at once, reconnects on the relay; resolves when back on the relay. */
    const moveAndDie = async (count: number) => {
      await until(() => sockets.length === count + 1, "a move's socket");
      const direct = sockets[count]!;
      direct.open();
      await wait(1);
      record.path = "relay";
      const diedAt = Date.now();
      direct.close(1006, "");
      await until(() => sockets.length === count + 2, "the reconnect");
      sockets[count + 1]!.open();
      return diedAt;
    };
    const firstDeath = await moveAndDie(1);
    const secondDeath = await moveAndDie(3);
    const firstWait = secondDeath - firstDeath;
    // After the second quick death the wait has doubled: no move's socket before twice the first backoff.
    expect(firstWait).toBeGreaterThanOrEqual(60);
    await wait(100);
    expect(sockets).toHaveLength(5);
    await until(() => sockets.length === 6, "the third move, after the doubled backoff");
    expect(Date.now() - secondDeath).toBeGreaterThanOrEqual(120);
    expect(record.moved).toBe(2);
    transport.close();
  });

  test("going away gives a move up; a network change starts the count over", async () => {
    const { transport, relay, record } = await onRelay([true]);
    await moveSocket();
    transport.setAway(true);
    expect(record.abandoned).toBe(1);
    expect(relay.readyState).toBe(1);
    transport.setAway(false);
    transport.networkChanged(true);
    // Three checks in a row again before the next socket.
    const checks = record.checks;
    await until(() => sockets.length === 3, "the next move's socket");
    expect(record.checks - checks).toBe(3);
    transport.close();
  });

  test("a connection that is not on the relay never checks for another path", async () => {
    const { moves, record } = fakeMoves([true]);
    record.path = "lan";
    const transport = new Transport(server, undefined, moves);
    transport.connect();
    await socketsReach(1);
    sockets[0]!.open();
    await wait(30);
    expect(record.checks).toBe(0);
    transport.close();
  });
});
