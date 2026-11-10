import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { FakeSocket, installMemoryPlatform } from "../testing/memoryPlatform";
import { Transport, transportTiming } from "../transport";
import { dshInteraction, nextDSHPollDelayMs, watchDSHInteractions, type DSHInteractionSnapshot } from "./dshInteractions";

describe("nextDSHPollDelayMs", () => {
  test("a reply with a revision is followed at once because the daemon holds the next request", () => {
    expect(nextDSHPollDelayMs({ ok: true, rev: 4 })).toBe(0);
    expect(nextDSHPollDelayMs({ ok: true, rev: 0 })).toBe(0);
  });

  test("a daemon that does not hold requests is asked at the fixed pace", () => {
    expect(nextDSHPollDelayMs({ ok: true })).toBe(1200);
  });

  test("failures back off and stop at the cap", () => {
    const delays = [1, 2, 3, 4, 5, 6, 20].map((failures) => nextDSHPollDelayMs({ ok: false, failures }));
    expect(delays).toEqual([1200, 2400, 4800, 9600, 19200, 30000, 30000]);
  });
});

const server = { id: "s1", name: "Desk", url: "ws://127.0.0.1:9/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const wait = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));
const saved = { ...transportTiming };
let sockets: FakeSocket[];
beforeEach(() => {
  sockets = [];
  installMemoryPlatform({ openSocket: (url, protocols) => { const s = new FakeSocket(url, protocols); sockets.push(s); return s; } });
});
afterEach(() => Object.assign(transportTiming, saved));

async function connected() {
  const transport = new Transport(server);
  transport.connect();
  await wait();
  const socket = sockets.at(-1)!;
  socket.open();
  const last = () => JSON.parse(socket.sent.at(-1)!);
  return { transport, socket, last };
}

const run = { workerId: "w1", processId: 7, startedAt: 1700 };

describe("DSH requests over the connection", () => {
  test("the watcher follows each revision at once and reports what is pending", async () => {
    const { transport, socket, last } = await connected();
    const seen: (DSHInteractionSnapshot | null)[] = [];
    const stop = watchDSHInteractions(transport, run, (snapshot) => seen.push(snapshot));
    await wait();
    expect(last()).toMatchObject({ type: "dsh_interaction", worker_id: "w1", process_id: 7, started_at: 1700 });
    expect(last().dsh_after_rev).toBeUndefined();
    socket.receive({ type: "dsh_interaction", request_id: last().request_id, result: { epoch: "e1", connected: true, items: [], rev: 3 } });
    await wait(10);
    expect(last().dsh_after_rev).toBe(3);
    expect(seen).toEqual([{ epoch: "e1", connected: true, items: [], rev: 3 }]);
    stop();
    transport.close();
  });

  test("an answer settles on accepted", async () => {
    const { transport, socket, last } = await connected();
    const done = dshInteraction(transport, run, { epoch: "e1", rpcId: "r1", outcome: "allowed-once" });
    expect(last().dsh_answer).toEqual({ epoch: "e1", rpcId: "r1", outcome: "allowed-once" });
    socket.receive({ type: "dsh_interaction", request_id: last().request_id, result: { accepted: true } });
    expect(await done).toEqual({ accepted: true });
    transport.close();
  });
});
