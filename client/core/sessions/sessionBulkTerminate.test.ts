import { describe, expect, test } from "bun:test";
import {
  createSessionTerminationEntries,
  SessionTerminationBatch,
  sessionTerminationConfirmMessage,
  sessionTerminationSummaryMessage,
  summarizeSessionTermination,
  SESSION_TERMINATION_OFFLINE_MESSAGE,
  SESSION_TERMINATION_TIMEOUT_MESSAGE,
  type SessionTerminationEntry,
  type SessionTerminationSummary,
  type SessionTerminationTransport,
} from "./sessionBulkTerminate";

import type { Frame, Transport } from "../transport";

// The current computer's Transport is what the UI passes.
export type TransportIsATerminationTransport = Transport extends SessionTerminationTransport ? true : never;
const transportFits: TransportIsATerminationTransport = true;
void transportFits;

interface KillRecord {
  workerId: string;
  /** Settles the request as the Transport would on the correlated reply. */
  reply(frame?: Partial<Frame>): void;
  /** Fails it as the Transport would on a correlated `error` frame, a timeout or a disconnect. */
  fail(message: string): void;
}

/** Stands in for core/transport.ts's Transport: it correlates replies itself, so the fake hands them out per request. */
class FakeTransport implements SessionTerminationTransport {
  kills: KillRecord[] = [];
  bodies: Record<string, unknown>[] = [];
  connected = true;
  readonly listeners = new Map<string, Set<(frame: Frame) => void>>();

  request(type: string, body: Record<string, unknown>, options: { reply: string; timeoutMs?: number }): Promise<Frame> {
    expect(type).toBe("kill_worker");
    expect(options.reply).toBe("worker_killed");
    this.bodies.push(body);
    if (!this.connected) return Promise.reject(new Error("Daemon is not connected."));
    return new Promise<Frame>((resolve, reject) => {
      this.kills.push({
        workerId: String(body.worker_id),
        reply: (frame) => resolve({ type: "worker_killed", worker_id: body.worker_id, ...frame }),
        fail: (message) => reject(new Error(message)),
      });
    });
  }

  on(type: string, fn: (frame: Frame) => void): () => void {
    const set = this.listeners.get(type) ?? new Set();
    set.add(fn);
    this.listeners.set(type, set);
    return () => set.delete(fn);
  }

  emit(type: string, data: Record<string, unknown>): void {
    for (const handler of this.listeners.get(type) ?? []) {
      handler({ type, ...data });
    }
  }

  listenerCount(type: string): number {
    return this.listeners.get(type)?.size ?? 0;
  }
}

const targets = [
  { sessionKey: "[\"srv-a\",\"agent-1\"]", serverId: "srv-a", workerId: "agent-1" },
  { sessionKey: "[\"srv-a\",\"agent-2\"]", serverId: "srv-a", workerId: "agent-2" },
  { sessionKey: "[\"srv-a\",\"agent-3\"]", serverId: "srv-a", workerId: "agent-3" },
];

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
/** Lets the request promises settle. */
const flush = () => sleep(0);

function settleAll(transport: FakeTransport): void {
  // Fail agent-2, succeed the rest via archived events.
  for (const kill of transport.kills) {
    if (kill.workerId === "agent-2") {
      kill.fail("session still live");
    } else {
      transport.emit("worker_session_archived", {
        worker_session: { id: kill.workerId },
      });
    }
  }
}

describe("createSessionTerminationEntries", () => {
  test("captures stable IDs only and starts fully pending", () => {
    const entries = createSessionTerminationEntries(targets);
    expect(entries).toEqual(
      targets.map((target) => ({ ...target, status: "pending" })),
    );
  });
});

describe("summarizeSessionTermination", () => {
  test("counts outcomes and reports failures in submission order", () => {
    const entries: SessionTerminationEntry[] = [
      { ...targets[0], status: "succeeded" },
      { ...targets[1], status: "failed", error: "boom" },
      { ...targets[2], status: "pending" },
    ];
    const summary = summarizeSessionTermination(entries);
    expect(summary).toEqual({
      total: 3,
      succeeded: 1,
      failed: 1,
      pending: 1,
      running: true,
      failedEntries: [entries[1]],
    });
  });
});

describe("SessionTerminationBatch.start", () => {
  test("submits every entry exactly once, naming only the Session", () => {
    const transport = new FakeTransport();
    const settled: SessionTerminationSummary[] = [];
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries(targets),
      onSettled: (summary) => settled.push(summary),
    });
    expect(batch.start()).toBe(true);
    expect(transport.kills).toHaveLength(3);
    // The Transport adds request_id; no serverId goes on the wire.
    expect(transport.bodies).toEqual([
      { worker_id: "agent-1" },
      { worker_id: "agent-2" },
      { worker_id: "agent-3" },
    ]);
  });

  test("second start returns false and never resubmits (duplicate prevention)", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries(targets),
      onSettled: () => undefined,
    });
    expect(batch.start()).toBe(true);
    const killCount = transport.kills.length;
    expect(batch.start()).toBe(false);
    expect(transport.kills).toHaveLength(killCount);
  });

  test("offline transport failure settles as retryable", async () => {
    const transport = new FakeTransport();
    transport.connected = false;
    const settled: SessionTerminationSummary[] = [];
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: (summary) => settled.push(summary),
    });
    batch.start();
    await flush();
    expect(batch.summary).toMatchObject({
      total: 1,
      succeeded: 0,
      failed: 1,
      pending: 0,
      running: false,
    });
    expect(batch.summary.failedEntries[0].error).toBe(
      SESSION_TERMINATION_OFFLINE_MESSAGE,
    );
    expect(settled.at(-1)?.running).toBe(false);
  });

  test("a disconnect while waiting fails the entry as offline", async () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
    });
    batch.start();
    transport.kills[0].fail("Daemon is not connected.");
    await flush();
    expect(batch.summary.failedEntries[0].error).toBe(
      SESSION_TERMINATION_OFFLINE_MESSAGE,
    );
  });
});

describe("SessionTerminationBatch settlement", () => {
  test("an error reply fails that entry only, keeping the daemon's message", async () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0], targets[1]]),
      onSettled: () => undefined,
    });
    batch.start();
    transport.kills.find((k) => k.workerId === "agent-1")!.fail("session still live");
    await flush();
    const summary = batch.summary;
    expect(summary.failed).toBe(1);
    expect(summary.succeeded).toBe(0);
    expect(summary.pending).toBe(1);
    expect(summary.failedEntries[0].error).toBe("session still live");
    expect(summary.failedEntries[0].sessionKey).toBe(targets[0].sessionKey);
  });

  test("worker_session_archived settles the matching Session as success", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
    });
    batch.start();
    transport.emit("worker_session_archived", {
      worker_session: { id: "agent-1" },
    });
    expect(batch.summary.succeeded).toBe(1);
    expect(batch.summary.running).toBe(false);
  });

  test("worker_killed answering the request settles as success", async () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0], targets[2]]),
      onSettled: () => undefined,
    });
    batch.start();
    transport.kills[0].reply();
    await flush();
    expect(batch.summary.succeeded).toBe(1);
    expect(batch.summary.pending).toBe(1);
    transport.kills[1].reply();
    await flush();
    expect(batch.summary.succeeded).toBe(2);
    expect(batch.summary.running).toBe(false);
  });

  test("a worker_session_list proves nothing about termination", () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
    });
    batch.start();
    expect(transport.listenerCount("worker_session_list")).toBe(0);
    transport.emit("worker_session_list", {
      worker_sessions: [],
    });
    expect(batch.summary.pending).toBe(1);
  });

  test("settlement is first-wins: archived then late error keeps success", async () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
    });
    batch.start();
    transport.emit("worker_session_archived", {
      worker_session: { id: "agent-1" },
    });
    transport.kills[0].fail("late failure");
    await flush();
    expect(batch.summary.succeeded).toBe(1);
    expect(batch.summary.failed).toBe(0);
  });

  test("timeout without any acknowledgement fails as retryable", async () => {
    const transport = new FakeTransport();
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: () => undefined,
      timeoutMs: 10,
    });
    batch.start();
    expect(batch.summary.pending).toBe(1);
    await sleep(40);
    expect(batch.summary.failed).toBe(1);
    expect(batch.summary.failedEntries[0].error).toBe(
      SESSION_TERMINATION_TIMEOUT_MESSAGE,
    );
    expect(batch.summary.running).toBe(false);
  });

  test("partial failure: successes settle, failures reported accurately", async () => {
    const transport = new FakeTransport();
    const finalSummaries: SessionTerminationSummary[] = [];
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries(targets),
      onSettled: (summary) => {
        if (summary.pending === 0) finalSummaries.push(summary);
      },
    });
    batch.start();
    settleAll(transport);
    await flush();
    expect(finalSummaries).toHaveLength(1);
    const final = finalSummaries[0];
    expect(final.total).toBe(3);
    expect(final.succeeded).toBe(2);
    expect(final.failed).toBe(1);
    expect(final.failedEntries.map((e) => e.workerId)).toEqual(["agent-2"]);
    expect(final.running).toBe(false);
    // Never reports all succeeded when only some did.
    expect(final.succeeded).not.toBe(final.total);
  });

  test("all success reports a single settled summary", () => {
    const transport = new FakeTransport();
    const summaries: SessionTerminationSummary[] = [];
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0], targets[1]]),
      onSettled: (summary) => summaries.push(summary),
    });
    batch.start();
    for (const kill of transport.kills) {
      transport.emit("worker_session_archived", {
        worker_session: { id: kill.workerId },
      });
    }
    expect(batch.summary.succeeded).toBe(2);
    expect(batch.summary.failed).toBe(0);
    const finals = summaries.filter((summary) => !summary.running);
    expect(finals).toHaveLength(1);
    expect(finals[0].succeeded).toBe(2);
  });
});

describe("SessionTerminationBatch dispose", () => {
  test("removes listeners and stops timers", async () => {
    const transport = new FakeTransport();
    const summaries: SessionTerminationSummary[] = [];
    const batch = new SessionTerminationBatch({
      transport,
      entries: createSessionTerminationEntries([targets[0]]),
      onSettled: (summary) => summaries.push(summary),
      timeoutMs: 10,
    });
    batch.start();
    expect(transport.listenerCount("worker_session_archived")).toBe(1);
    expect(summaries).toHaveLength(1); // initial running summary from start()
    batch.dispose();
    expect(transport.listenerCount("worker_session_archived")).toBe(0);
    // No event, reply or timer may settle after dispose.
    transport.emit("worker_session_archived", {
      worker_session: { id: "agent-1" },
    });
    transport.kills[0].reply();
    await sleep(40);
    expect(summaries).toHaveLength(1);
    expect(batch.summary.pending).toBe(1);
  });
});

describe("confirmation and summary copy", () => {
  test("confirmation names the exact selected count", () => {
    expect(sessionTerminationConfirmMessage(1)).toContain(
      "This session will be terminated.",
    );
    expect(sessionTerminationConfirmMessage(3)).toContain(
      "These 3 sessions will be terminated.",
    );
  });

  test("partial failure message never claims full success", () => {
    const entries: SessionTerminationEntry[] = [
      { ...targets[0], status: "succeeded" },
      { ...targets[1], status: "failed", error: "boom" },
    ];
    const summary = summarizeSessionTermination(entries);
    const message = sessionTerminationSummaryMessage(summary, ["agent-2"]);
    expect(message).toContain("1 of 2 terminated");
    expect(message).toContain("1 session failed");
    expect(message).toContain("agent-2");
    expect(message).toContain("remain selected");
    expect(message).not.toContain("all");
  });

  test("total failure message names the count and retry", () => {
    const entries: SessionTerminationEntry[] = [
      { ...targets[0], status: "failed", error: "boom" },
    ];
    const summary = summarizeSessionTermination(entries);
    const message = sessionTerminationSummaryMessage(summary, []);
    expect(message).toContain("Could not terminate 1 session");
    expect(message).toContain("Retry to terminate");
  });

  test("empty summary yields no failure message", () => {
    const entries: SessionTerminationEntry[] = [
      { ...targets[0], status: "succeeded" },
    ];
    expect(sessionTerminationSummaryMessage(summarizeSessionTermination(entries), [])).toBe("");
  });
});
