import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { FakeSocket, installMemoryPlatform } from "../testing/memoryPlatform";
import { Transport, transportTiming } from "../transport";
import { answerChoice, stopTurn } from "./turn";

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

describe("turn controls", () => {
  test("Stop sends a pause to the Session and settles on action_sent", async () => {
    const { transport, socket, last } = await connected();
    const done = stopTurn(transport, "w1");
    expect(last()).toMatchObject({ type: "send_action", worker_id: "w1", action: "pause" });
    socket.receive({ type: "action_sent", request_id: last().request_id });
    await done;
    transport.close();
  });

  test("an answer to a question that passed rejects with its code", async () => {
    const { transport, socket, last } = await connected();
    const done = answerChoice(transport, "host", "call-1", [{ selected: [1], other: "" }]);
    expect(last()).toMatchObject({ type: "answer_choice", worker_id: "host", choice_answer: { call_id: "call-1", answers: [{ selected: [1], other: "" }] } });
    socket.receive({ type: "error", request_id: last().request_id, code: "answer_choice_gone", message: "gone" });
    await expect(done).rejects.toMatchObject({ code: "answer_choice_gone" });
    transport.close();
  });
});
