import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { FakeSocket, installMemoryPlatform, memoryStore } from "../testing/memoryPlatform";
import { Transport, transportTiming } from "../transport";
import { Chat } from "./chat";
import { createOutbox } from "./outboxRuntime";

const server = { id: "s1", name: "Desk", url: "ws://127.0.0.1:9/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const target = { targetId: "brain-host", conversationScopeKey: "brain-thread:t1" };
const wait = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));
/** Waits until `ready()` holds (a timer-driven step finished), at most 4 s (inside bun's 5 s): a fixed sleep, or a short limit, loses under load. */
const until = async (ready: () => boolean, ms = 4000) => {
  const end = Date.now() + ms;
  while (!ready() && Date.now() < end) await wait(2);
};
const saved = { ...transportTiming };
let sockets: FakeSocket[];

beforeEach(() => {
  sockets = [];
  installMemoryPlatform({
    openSocket: (url, protocols) => {
      const socket = new FakeSocket(url, protocols);
      sockets.push(socket);
      return socket;
    },
  });
  // Quick retries; the silence watchdog (transport.test's to prove) never fires here, as a busy runner's late timers would read as silence.
  Object.assign(transportTiming, { firstRetryMs: 10, maxRetryMs: 40, checkMs: 10, silentLimitMs: 60_000 });
});
afterEach(() => Object.assign(transportTiming, saved));

function sent(socket: FakeSocket, type: string) {
  return socket.sent.map((text) => JSON.parse(text)).filter((frame) => frame.type === type);
}

async function open(storage = memoryStore()) {
  const transport = new Transport(server);
  const outbox = createOutbox(storage, (id) => (id === server.id ? transport : null));
  transport.on("connected", () => outbox.onConnected(server.id));
  transport.on("disconnected", () => outbox.onDisconnected(server.id));
  transport.connect();
  const chat = new Chat({ serverId: server.id, transport, target, outbox });
  // Open every socket being tried (one may be a reconnect of another file's transport, which bun runs in the
  // same process), until the chat's subscribe goes out on one.
  const subscribed = () => sockets.find((s) => s.readyState === 1 && sent(s, "codex_conversation_subscribe").length > 0);
  await until(() => {
    for (const s of sockets) if (s.readyState === 0) s.open();
    return Boolean(subscribed());
  });
  return { transport, outbox, chat, storage, socket: subscribed()! };
}

const event = (id: string, seq: number, kind: string, body: string, extra: object = {}) => ({ id, seq, kind, body, timestamp: new Date(1_700_000_000_000 + seq * 1000).toISOString(), ...extra });

function snapshot(socket: FakeSocket, revision: number, events: object[]) {
  const request_id = sent(socket, "codex_conversation_subscribe").at(-1).request_id;
  socket.receive({ type: "codex_conversation_snapshot", request_id, conversation_id: "c1", revision, generation: "g1", conversation: { available: true, events } });
  return request_id;
}

describe("Chat", () => {
  test("follows the conversation once connected, in Brain's thread scope, and applies revisions", async () => {
    const { chat, socket, transport } = await open();
    const [subscribe] = sent(socket, "codex_conversation_subscribe");
    expect(subscribe).toMatchObject({ target_id: "brain-host", conversation_scope_key: "brain-thread:t1" });
    const request_id = snapshot(socket, 1, [event("u1", 1, "user_message", "hi", { role: "user" })]);
    expect(chat.current.conversation?.events.map((e) => e.id)).toEqual(["u1"]);
    socket.receive({ type: "codex_conversation_delta", request_id, conversation_id: "c1", revision: 2, base_revision: 1, generation: "g1", upserts: [event("a1", 2, "assistant_message", "Hel", { role: "assistant", partial: true })], deletes: [] });
    socket.receive({ type: "codex_conversation_delta", request_id, conversation_id: "c1", revision: 3, base_revision: 2, generation: "g1", upserts: [event("a1", 2, "assistant_message", "Hello", { role: "assistant" })], deletes: [] });
    expect(chat.current.conversation?.events.map((e) => [e.id, e.body])).toEqual([["u1", "hi"], ["a1", "Hello"]]);
    // Another subscription's frames are not this chat's.
    socket.receive({ type: "codex_conversation_delta", request_id: "other", conversation_id: "c1", revision: 4, base_revision: 3, generation: "g1", upserts: [event("x", 3, "assistant_message", "nope", { role: "assistant" })], deletes: [] });
    expect(chat.current.conversation?.events).toHaveLength(2);
    chat.close();
    expect(sent(socket, "codex_conversation_unsubscribe")).toHaveLength(1);
    transport.close();
  });

  test("a revision gap asks for the conversation again under a new subscription", async () => {
    const { chat, socket, transport } = await open();
    const first = snapshot(socket, 1, [event("u1", 1, "user_message", "hi", { role: "user" })]);
    socket.receive({ type: "codex_conversation_delta", request_id: first, conversation_id: "c1", revision: 5, base_revision: 4, generation: "g1", upserts: [], deletes: [] });
    const subscribes = sent(socket, "codex_conversation_subscribe");
    expect(subscribes).toHaveLength(2);
    expect(subscribes[1].request_id).not.toBe(first);
    expect(sent(socket, "codex_conversation_unsubscribe").map((f) => f.request_id)).toEqual([first]);
    snapshot(socket, 5, [event("u1", 1, "user_message", "hi", { role: "user" }), event("a1", 2, "assistant_message", "Hello", { role: "assistant" })]);
    expect(chat.current.conversation?.events).toHaveLength(2);
    chat.close();
    transport.close();
  });

  test("subscribes again after a reconnect", async () => {
    const { chat, socket, transport } = await open();
    snapshot(socket, 1, []);
    socket.close(1006, "gone");
    // Open whichever socket the transport is trying now (a slow runner can replace a reconnect with the next one).
    const before = sockets.length;
    const fresh = () => sockets.slice(before);
    await until(() => {
      for (const s of fresh()) if (s.readyState === 0) s.open();
      return fresh().some((s) => sent(s, "codex_conversation_subscribe").length > 0);
    });
    expect(fresh().flatMap((s) => sent(s, "codex_conversation_subscribe"))).toHaveLength(1);
    chat.close();
    transport.close();
  });

  test("a send shows at once, goes out through the outbox under one id, and settles on input_sent", async () => {
    const { chat, socket, transport } = await open();
    snapshot(socket, 1, []);
    chat.send("  Plan my week  ");
    expect(chat.current.pendingUserMessages).toHaveLength(1);
    expect(chat.current.pendingUserMessages[0]).toMatchObject({ body: "Plan my week", lifecycle: "pending" });
    await wait();
    const [input] = sent(socket, "send_input");
    expect(input).toMatchObject({ worker_id: "brain-host", conversation_scope_key: "brain-thread:t1" });
    expect(input.request_id).toBe(chat.current.pendingUserMessages[0].dispatchRequestId);
    socket.receive({ type: "input_sent", request_id: input.request_id });
    await wait();
    expect(chat.current.pendingUserMessages.map((m) => m.lifecycle)).toEqual(["pending"]);
    // The provider's echo of the message replaces the row.
    socket.receive({ type: "codex_conversation_delta", request_id: sent(socket, "codex_conversation_subscribe").at(-1).request_id, conversation_id: "c1", revision: 2, base_revision: 1, generation: "g1", upserts: [event("u1", 1, "user_message", "Plan my week", { role: "user" })], deletes: [] });
    expect(chat.current.pendingUserMessages).toHaveLength(0);
    expect(chat.current.conversation?.events.map((e) => e.body)).toEqual(["Plan my week"]);
    chat.close();
    transport.close();
  });

  test("a message sent offline waits, then goes out once the computer is back", async () => {
    const { chat, socket, transport } = await open();
    snapshot(socket, 1, []);
    socket.close(1006, "gone");
    chat.send("while offline");
    await wait();
    expect(chat.current.pendingUserMessages[0]?.lifecycle).toBe("pending");
    // The computer comes back: open whichever socket the transport is trying now (under load a
    // reconnect can be replaced by the next one) until the queued message goes out on one of them.
    const before = sockets.length;
    const fresh = () => sockets.slice(before);
    await until(() => {
      for (const s of fresh()) if (s.readyState === 0) s.open();
      return fresh().some((s) => sent(s, "send_input").length > 0);
    }, 3000);
    const inputs = fresh().flatMap((s) => sent(s, "send_input"));
    expect(inputs).toHaveLength(1);
    expect(inputs[0].text).toBe("while offline");
    chat.close();
    transport.close();
  });

  test("messages the app queued before it closed come back as rows", async () => {
    const storage = memoryStore({
      "mewla:v1:send_outbox": JSON.stringify([
        { id: "q1", serverId: "s1", workerId: "brain-host", conversationScopeKey: "brain-thread:t1", text: "from before", createdAt: Date.now(), state: "undeliverable", attempts: 4, failureCode: "stale_receipt_invalidated" },
        { id: "q2", serverId: "s1", workerId: "brain-host", conversationScopeKey: "brain-thread:other", text: "another thread", createdAt: Date.now(), state: "undeliverable", attempts: 4 },
      ]),
    });
    const { chat, transport } = await open(storage);
    expect(chat.current.pendingUserMessages.map((m) => [m.body, m.lifecycle])).toEqual([["from before", "failed"]]);
    chat.actOnFailed(chat.current.pendingUserMessages[0].id, "discard");
    expect(chat.current.pendingUserMessages).toHaveLength(0);
    chat.close();
    transport.close();
  });
});
