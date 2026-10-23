import { describe, expect, test } from "bun:test";
import {
  MessageOutbox,
  OUTBOX_ACK_STALL_MS,
  OUTBOX_MAX_ATTEMPTS,
  OUTBOX_RETRY_BACKOFF_MS,
  OUTBOX_UNCERTAIN_PATIENCE_MS,
  type OutboxClock,
  type OutboxEntry,
  type OutboxEvent,
  type OutboxStore,
  type OutboxTransport,
} from "./messageOutbox";
import type {
  SendAttachment,
  StructuredCommandOutcome,
  StructuredCommandReceipt,
} from "./structuredWebSocketTransport";

const tick = async () => {
  for (let i = 0; i < 20; i++) await Promise.resolve();
};

class FakeClock implements OutboxClock {
  time = 1_000_000;
  private nextHandle = 1;
  private timers = new Map<number, { at: number; run: () => void }>();
  now() {
    return this.time;
  }
  setTimer(run: () => void, ms: number) {
    const handle = this.nextHandle++;
    this.timers.set(handle, { at: this.time + ms, run });
    return handle;
  }
  clearTimer(handle: unknown) {
    this.timers.delete(handle as number);
  }
  async advance(ms: number) {
    const target = this.time + ms;
    for (;;) {
      const due = [...this.timers.entries()]
        .filter(([, timer]) => timer.at <= target)
        .sort((a, b) => a[1].at - b[1].at)[0];
      if (!due) break;
      this.timers.delete(due[0]);
      this.time = Math.max(this.time, due[1].at);
      due[1].run();
      await tick();
    }
    this.time = target;
    await tick();
  }
}

type Sent = {
  serverId: string;
  workerId: string;
  text: string;
  attachments?: SendAttachment[];
  requestId: string;
  settle(outcome: StructuredCommandOutcome): void;
};

class FakeTransport implements OutboxTransport {
  connected = new Set<string>();
  sent: Sent[] = [];
  dropped: string[] = [];
  isConnected(serverId: string) {
    return this.connected.has(serverId);
  }
  sendInput(
    serverId: string,
    workerId: string,
    text: string,
    options: { requestId: string; attachments?: SendAttachment[] },
  ): StructuredCommandReceipt {
    if (!this.connected.has(serverId)) {
      throw new Error("Daemon is not connected.");
    }
    let resolve!: (outcome: StructuredCommandOutcome) => void;
    const outcome = new Promise<StructuredCommandOutcome>((r) => {
      resolve = r;
    });
    this.sent.push({
      serverId,
      workerId,
      text,
      attachments: options.attachments,
      requestId: options.requestId,
      settle: resolve,
    });
    return { requestId: options.requestId, outcome };
  }
  dropConnection(serverId: string, reason: string) {
    this.dropped.push(`${serverId}:${reason}`);
  }
  /** What the daemon sees on the wire, in order. */
  get ids() {
    return this.sent.map((sent) => sent.requestId);
  }
  /** The connection dies: every unanswered send reports it. */
  drop(serverId: string) {
    this.connected.delete(serverId);
    for (const sent of this.sent) sent.settle({ kind: "connection_closed" });
  }
}

class MemoryStore implements OutboxStore {
  saved: OutboxEntry[] = [];
  constructor(initial: OutboxEntry[] = []) {
    this.saved = initial;
  }
  async load() {
    return this.saved.map((entry) => ({ ...entry }));
  }
  async save(entries: OutboxEntry[]) {
    this.saved = entries.map((entry) => ({ ...entry }));
  }
}

function setup(initial: OutboxEntry[] = [], idPrefix = "id") {
  const clock = new FakeClock();
  const transport = new FakeTransport();
  const store = new MemoryStore(initial);
  let counter = 0;
  const outbox = new MessageOutbox(transport, store, clock, () => `${idPrefix}${++counter}`);
  const events: OutboxEvent[] = [];
  outbox.subscribe((event) => events.push(event));
  const connect = async (serverId = "s1") => {
    transport.connected.add(serverId);
    outbox.onConnected(serverId);
    await tick();
  };
  const disconnect = async (serverId = "s1") => {
    transport.drop(serverId);
    outbox.onDisconnected(serverId);
    await tick();
  };
  const send = async (text: string, workerId = "w1", serverId = "s1") => {
    const entry = outbox.enqueue({
      serverId,
      workerId,
      text,
    });
    await tick();
    return entry;
  };
  return { clock, transport, store, outbox, events, connect, disconnect, send };
}

describe("MessageOutbox", () => {
  test("a message sent offline waits and goes out once the connection is back", async () => {
    const { transport, outbox, send, connect, store } = setup();
    const entry = await send("hello");
    expect(transport.sent).toHaveLength(0);
    expect(outbox.get(entry.id)?.state).toBe("queued");
    expect(store.saved.map((saved) => saved.id)).toEqual([entry.id]);

    await connect();
    expect(transport.ids).toEqual([entry.id]);
    transport.sent[0].settle({ kind: "sent" });
    await tick();
    expect(outbox.get(entry.id)).toBeUndefined();
    expect(store.saved).toEqual([]);
  });

  test("a lost connection never fails the message: it is sent again under the same id", async () => {
    const { transport, send, connect, disconnect, outbox, events } = setup();
    await connect();
    const entry = await send("hello");
    expect(transport.ids).toEqual([entry.id]);

    await disconnect();
    expect(outbox.get(entry.id)?.state).toBe("queued");
    expect(events.some((event) => event.kind === "undeliverable")).toBe(false);

    await connect();
    expect(transport.ids).toEqual([entry.id, entry.id]);
    transport.sent[1].settle({ kind: "sent" });
    await tick();
    expect(events.filter((event) => event.kind === "accepted")).toHaveLength(1);
    expect(outbox.entriesFor("s1", "w1")).toEqual([]);
  });

  test("messages in a row keep their order, one at a time per worker", async () => {
    const { transport, send, connect } = setup();
    await connect();
    const first = await send("one");
    const second = await send("two");
    const third = await send("three");
    const other = await send("parallel", "w2");
    // The second waits for the first; another worker is not held up.
    expect(transport.sent.map((sent) => sent.text)).toEqual(["one", "parallel"]);
    transport.sent[0].settle({ kind: "sent" });
    await tick();
    expect(transport.sent.map((sent) => sent.text)).toEqual(["one", "parallel", "two"]);
    transport.sent[2].settle({ kind: "sent" });
    await tick();
    transport.sent[3].settle({ kind: "sent" });
    await tick();
    expect(transport.ids).toEqual([first.id, other.id, second.id, third.id]);
  });

  test("a refused message is retried under the same id before anything behind it goes out", async () => {
    const { transport, send, connect, clock, events } = setup();
    await connect();
    const first = await send("one");
    await send("two");
    transport.sent[0].settle({
      kind: "failed",
      failure: { requestId: first.id, code: "input_not_submitted", message: "no" },
    });
    await tick();
    expect(transport.sent).toHaveLength(1);
    await clock.advance(OUTBOX_RETRY_BACKOFF_MS[0]);
    expect(transport.ids).toEqual([first.id, first.id]);
    transport.sent[1].settle({ kind: "sent" });
    await tick();
    expect(transport.sent.map((sent) => sent.text)).toEqual(["one", "one", "two"]);
    expect(events.some((event) => event.kind === "undeliverable")).toBe(false);
  });

  test("a message the daemon keeps refusing stops waiting, and the queue moves on", async () => {
    const { transport, send, connect, clock, events, outbox } = setup();
    await connect();
    const first = await send("one");
    await send("two");
    for (let attempt = 0; attempt < OUTBOX_MAX_ATTEMPTS; attempt++) {
      transport.sent[transport.sent.length - 1].settle({
        kind: "failed",
        failure: { requestId: first.id, code: "send_input_failed", message: "gone" },
      });
      await tick();
      await clock.advance(OUTBOX_RETRY_BACKOFF_MS.at(-1)!);
    }
    expect(outbox.get(first.id)?.state).toBe("undeliverable");
    expect(events.filter((event) => event.kind === "undeliverable")).toHaveLength(1);
    expect(transport.sent.at(-1)?.text).toBe("two");
  });

  test("a stale receipt continues the same message under a fresh id", async () => {
    const { transport, send, connect, events } = setup();
    await connect();
    const entry = await send("hello");
    const firstId = entry.id;
    transport.sent[0].settle({
      kind: "failed",
      failure: { requestId: firstId, code: "stale_receipt_invalidated", message: "stale" },
    });
    await tick();
    const reidentified = events.find((event) => event.kind === "reidentified");
    expect(reidentified && reidentified.kind === "reidentified" && reidentified.previousId).toBe(firstId);
    expect(transport.sent).toHaveLength(2);
    expect(transport.sent[1].requestId).not.toBe(firstId);
    expect(transport.sent[1].requestId).toBe(entry.id);
  });

  test("an unconfirmed delivery is never replayed; with no echo it asks the person", async () => {
    const { transport, send, connect, clock, outbox, events } = setup();
    await connect();
    const entry = await send("hello");
    transport.sent[0].settle({ kind: "pending", code: "input_uncertain" });
    await tick();
    expect(outbox.get(entry.id)?.state).toBe("uncertain");
    await disconnect_and_reconnect(transport, outbox);
    expect(transport.sent).toHaveLength(1);

    await clock.advance(OUTBOX_UNCERTAIN_PATIENCE_MS);
    expect(outbox.get(entry.id)?.state).toBe("undeliverable");
    expect(events.filter((event) => event.kind === "undeliverable")).toHaveLength(1);

    // One tap sends it again as a new message under a new id.
    const previousId = entry.id;
    const resent = outbox.resend(previousId)!;
    await tick();
    expect(resent.id).not.toBe(previousId);
    expect(transport.sent).toHaveLength(2);
    expect(transport.sent[1].requestId).toBe(resent.id);
  });

  test("a message delivered but never acknowledged is sent again and lands once on a daemon that dedupes by id", async () => {
    // The daemon's receipt ledger, reduced to its contract: one turn per id.
    const turns: string[] = [];
    const seen = new Set<string>();
    const daemonReceives = (sent: Sent, ackLost: boolean) => {
      if (!seen.has(sent.requestId)) {
        seen.add(sent.requestId);
        turns.push(sent.text);
      }
      if (!ackLost) sent.settle({ kind: "sent" });
    };
    const { transport, send, connect, disconnect, outbox } = setup();
    await connect();
    await send("你写的文案 AI 味道太重了");
    daemonReceives(transport.sent[0], true); // delivered, ack lost with the socket
    await disconnect();
    await connect();
    daemonReceives(transport.sent[1], false);
    await tick();
    expect(turns).toEqual(["你写的文案 AI 味道太重了"]);
    expect(outbox.entriesFor("s1", "w1")).toEqual([]);
  });

  test("the queue survives an app restart and goes out in order", async () => {
    const first = setup();
    await first.send("one");
    await first.send("two");
    const persisted = first.store.saved;
    expect(persisted.map((entry) => entry.text)).toEqual(["one", "two"]);

    const restarted = setup(persisted);
    await restarted.outbox.hydrate();
    await restarted.connect();
    expect(restarted.transport.sent.map((sent) => sent.text)).toEqual(["one"]);
    restarted.transport.sent[0].settle({ kind: "sent" });
    await tick();
    expect(restarted.transport.sent.map((sent) => sent.text)).toEqual(["one", "two"]);
  });

  test("typed attachments are queued, persisted and sent next to the text", async () => {
    const files = [{ name: "photo.png", path: "/uploads/photo.png", content_type: "image/png" }];
    const first = setup();
    first.outbox.enqueue({ serverId: "s1", workerId: "w1", text: "look", attachments: files });
    first.outbox.enqueue({ serverId: "s1", workerId: "w1", text: "plain", attachments: [] });
    await tick();
    expect(first.store.saved.map((entry) => entry.attachments)).toEqual([files, undefined]);

    const restarted = setup(first.store.saved);
    await restarted.outbox.hydrate();
    await restarted.connect();
    expect(restarted.transport.sent[0].text).toBe("look");
    expect(restarted.transport.sent[0].attachments).toEqual(files);
  });

  test("saving never erases what has not been loaded yet", async () => {
    const stored = setup();
    await stored.send("old");
    const restarted = setup(stored.store.saved, "later");
    // A new message arrives before the persisted queue was read back.
    restarted.outbox.enqueue({ serverId: "s1", workerId: "w1", text: "new" });
    await tick();
    await restarted.outbox.hydrate();
    await tick();
    expect(restarted.store.saved.map((entry) => entry.text).sort()).toEqual(["new", "old"]);
  });

  test("only the current server's messages are sent", async () => {
    const { transport, send, connect } = setup();
    await send("for A", "w1", "serverA");
    await send("for B", "w1", "serverB");
    await connect("serverB");
    expect(transport.sent.map((sent) => sent.text)).toEqual(["for B"]);
  });

  test("an answer that never comes on a live connection drops it so the send is repeated", async () => {
    const { transport, send, connect, clock } = setup();
    await connect();
    await send("hello");
    await clock.advance(OUTBOX_ACK_STALL_MS);
    expect(transport.dropped).toEqual(["s1:ack_stall"]);
  });

  test("discard removes a message for good", async () => {
    const { send, outbox, store } = setup();
    const entry = await send("hello");
    outbox.discard(entry.id);
    await tick();
    expect(outbox.get(entry.id)).toBeUndefined();
    expect(store.saved).toEqual([]);
  });
});

async function disconnect_and_reconnect(transport: FakeTransport, outbox: MessageOutbox) {
  transport.drop("s1");
  outbox.onDisconnected("s1");
  await tick();
  transport.connected.add("s1");
  outbox.onConnected("s1");
  await tick();
}
