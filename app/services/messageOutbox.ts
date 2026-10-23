import type {
  SendAttachment,
  StructuredCommandOutcome,
  StructuredCommandReceipt,
} from "./structuredWebSocketTransport";

export type { SendAttachment };

/**
 * The send queue behind every chat. A message joins the outbox the moment the
 * user sends it, gets its id there, and is sent, and sent again, until the
 * daemon has it. The daemon dedupes by that id, so a resend after a lost
 * acknowledgement never starts a second turn. A closed connection, a silent
 * one or a reconnect only means "send it again on the next one".
 *
 * Only a message the daemon proved it did not submit, over and over, or one it
 * could not prove either way for a long while, stops waiting and becomes
 * undeliverable. That is the one state the person ever has to touch.
 */

export type OutboxEntryState =
  /** Waiting to be sent, or sent and waiting for the daemon's answer. */
  | "queued"
  /**
   * The daemon may or may not have it and will not replay it blindly. The
   * provider's own echo normally settles it; if none arrives in time it
   * becomes undeliverable.
   */
  | "uncertain"
  /** Stopped. Only the person can send it again, as a new message, or delete it. */
  | "undeliverable";

export type OutboxEntry = {
  /** The client id: the request id the daemon dedupes on. */
  id: string;
  serverId: string;
  workerId: string;
  conversationScopeKey?: string;
  /** What the person wrote; the daemon renders it with the attachments. */
  text: string;
  attachments?: SendAttachment[];
  createdAt: number;
  state: OutboxEntryState;
  /** Proven-non-submitted attempts so far. */
  attempts: number;
  /** When it became uncertain. */
  uncertainSince?: number;
  failureCode?: string;
};

export type OutboxEnqueueInput = {
  /** A client id from `mintId`, when the caller must know it before enqueueing. */
  id?: string;
  serverId: string;
  workerId: string;
  conversationScopeKey?: string;
  text: string;
  attachments?: SendAttachment[];
};

export type OutboxEvent =
  /** The entry list for a server changed (enqueue, hydrate, resend, discard). */
  | { kind: "changed"; serverId: string }
  /** The daemon has the message; the provider's transcript will echo it. */
  | { kind: "accepted"; entry: OutboxEntry }
  | { kind: "uncertain"; entry: OutboxEntry }
  | { kind: "undeliverable"; entry: OutboxEntry }
  /** The daemon refused the id; the same message continues under a new one. */
  | { kind: "reidentified"; entry: OutboxEntry; previousId: string };

export type OutboxTransport = {
  /** True when this server is the live connection and its socket is open. */
  isConnected(serverId: string): boolean;
  sendInput(
    serverId: string,
    workerId: string,
    text: string,
    options: {
      attachments?: SendAttachment[];
      conversationScopeKey?: string;
      requestId: string;
    },
  ): StructuredCommandReceipt;
  /** Drop a connection that is not answering so the client reconnects at once. */
  dropConnection(serverId: string, reason: string): void;
};

export type OutboxStore = {
  load(): Promise<OutboxEntry[]>;
  save(entries: OutboxEntry[]): Promise<void>;
};

export type OutboxClock = {
  now(): number;
  setTimer(run: () => void, ms: number): unknown;
  clearTimer(handle: unknown): void;
};

/** Waits between attempts after the daemon proved it did not submit. */
export const OUTBOX_RETRY_BACKOFF_MS = [1_000, 3_000, 8_000] as const;
/** Attempts the daemon may refuse before the message stops waiting. */
export const OUTBOX_MAX_ATTEMPTS = OUTBOX_RETRY_BACKOFF_MS.length + 1;
/**
 * How long an unconfirmed delivery waits for the provider's echo before the
 * person is asked. Long enough for any provider to echo; short enough that a
 * message that really went nowhere does not sit there as a clock.
 */
export const OUTBOX_UNCERTAIN_PATIENCE_MS = 120_000;
/**
 * A send that gets no answer on a connection that is otherwise alive is sent
 * again on a fresh one: the daemon dedupes it. Provider admission can take
 * minutes while the agent is busy, so this is generous.
 */
export const OUTBOX_ACK_STALL_MS = 180_000;
/** A message waiting this long for a computer is not sent behind the person's back. */
export const OUTBOX_MAX_QUEUE_AGE_MS = 24 * 60 * 60 * 1000;

export const STALE_RECEIPT_CODE = "stale_receipt_invalidated";

export function newOutboxId(
  now: number = Date.now(),
  random: () => number = Math.random,
): string {
  return `${now.toString(36)}_${random().toString(36).slice(2, 8)}`;
}

type InFlight = {
  /** The connection generation the send was written on. */
  generation: number;
  stallTimer: unknown;
};

export class MessageOutbox {
  private entries: OutboxEntry[] = [];
  private readonly listeners = new Set<(event: OutboxEvent) => void>();
  private readonly inFlight = new Map<string, InFlight>();
  private readonly retryTimers = new Map<string, unknown>();
  private readonly uncertainTimers = new Map<string, unknown>();
  private readonly generations = new Map<string, number>();
  private hydrated: Promise<void> | undefined;
  private saveTail: Promise<void> = Promise.resolve();
  private drainScheduled = new Set<string>();

  constructor(
    private readonly transport: OutboxTransport,
    private readonly store: OutboxStore,
    private readonly clock: OutboxClock,
    private readonly newId: () => string = () => newOutboxId(clock.now()),
  ) {}

  subscribe(listener: (event: OutboxEvent) => void): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  /** Loads the persisted queue once. Safe to call from every entry point. */
  hydrate(): Promise<void> {
    this.hydrated ??= this.store.load().then(
      (loaded) => {
        const known = new Set(this.entries.map((entry) => entry.id));
        const now = this.clock.now();
        for (const entry of loaded) {
          if (known.has(entry.id)) continue;
          this.entries.push(entry);
          if (entry.state === "uncertain") {
            this.watchUncertain(entry, now);
          }
        }
        this.entries.sort((a, b) => a.createdAt - b.createdAt);
        const servers = new Set(loaded.map((entry) => entry.serverId));
        for (const serverId of servers) {
          this.emit({ kind: "changed", serverId });
          this.scheduleDrain(serverId);
        }
      },
      () => undefined,
    );
    return this.hydrated;
  }

  /** Entries for one conversation, oldest first. */
  entriesFor(
    serverId: string,
    workerId: string,
    conversationScopeKey?: string,
  ): OutboxEntry[] {
    return this.entries.filter(
      (entry) =>
        entry.serverId === serverId &&
        entry.workerId === workerId &&
        (entry.conversationScopeKey ?? "") === (conversationScopeKey ?? ""),
    );
  }

  get(id: string): OutboxEntry | undefined {
    return this.entries.find((entry) => entry.id === id);
  }

  /** A fresh client id for a message that is about to be enqueued. */
  mintId(): string {
    return this.newId();
  }

  /**
   * Takes a message into the queue and returns it with its id. The send
   * happens after the caller has had the chance to show the row.
   */
  enqueue(input: OutboxEnqueueInput): OutboxEntry {
    const entry: OutboxEntry = {
      id: input.id ?? this.newId(),
      serverId: input.serverId,
      workerId: input.workerId,
      ...(input.conversationScopeKey
        ? { conversationScopeKey: input.conversationScopeKey }
        : {}),
      text: input.text,
      ...(input.attachments && input.attachments.length > 0
        ? { attachments: input.attachments }
        : {}),
      createdAt: this.clock.now(),
      state: "queued",
      attempts: 0,
    };
    this.entries.push(entry);
    this.persist();
    this.emit({ kind: "changed", serverId: entry.serverId });
    this.scheduleDrain(entry.serverId);
    return entry;
  }

  /** The connection to `serverId` is open: send everything that is waiting. */
  onConnected(serverId: string) {
    this.generations.set(serverId, (this.generations.get(serverId) ?? 0) + 1);
    void this.hydrate().then(() => this.drain(serverId));
  }

  /** The connection closed: whatever was waiting on it will be sent again. */
  onDisconnected(serverId: string) {
    this.generations.set(serverId, (this.generations.get(serverId) ?? 0) + 1);
    for (const [id, flight] of this.inFlight) {
      const entry = this.get(id);
      if (entry?.serverId === serverId) {
        this.clock.clearTimer(flight.stallTimer);
        this.inFlight.delete(id);
      }
    }
  }

  /**
   * The person asked to send an undeliverable message again. It goes out as a
   * new message under a new id: the old id may be bound to a delivery the
   * daemon could not prove either way.
   */
  resend(id: string): OutboxEntry | undefined {
    const entry = this.get(id);
    if (!entry || entry.state === "queued") {
      return undefined;
    }
    const previousId = entry.id;
    this.clearTimers(previousId);
    entry.id = this.newId();
    entry.state = "queued";
    entry.attempts = 0;
    entry.createdAt = this.clock.now();
    delete entry.failureCode;
    delete entry.uncertainSince;
    // Back of the line: it is a new message now.
    this.entries = [...this.entries.filter((other) => other !== entry), entry];
    this.persist();
    this.emit({ kind: "reidentified", entry, previousId });
    this.emit({ kind: "changed", serverId: entry.serverId });
    this.scheduleDrain(entry.serverId);
    return entry;
  }

  discard(id: string) {
    const entry = this.get(id);
    if (!entry) {
      return;
    }
    this.clearTimers(id);
    this.entries = this.entries.filter((other) => other !== entry);
    this.persist();
    this.emit({ kind: "changed", serverId: entry.serverId });
  }

  /** Forgets everything held for a server, e.g. once it is removed. */
  forgetServer(serverId: string) {
    for (const entry of this.entries.filter((e) => e.serverId === serverId)) {
      this.clearTimers(entry.id);
    }
    this.entries = this.entries.filter((entry) => entry.serverId !== serverId);
    this.persist();
  }

  // Sends run on a later turn so the caller that enqueued can create its row
  // first: an answer must never arrive before the row it settles.
  private scheduleDrain(serverId: string) {
    if (this.drainScheduled.has(serverId)) {
      return;
    }
    this.drainScheduled.add(serverId);
    void Promise.resolve().then(() => {
      this.drainScheduled.delete(serverId);
      this.drain(serverId);
    });
  }

  private drain(serverId: string) {
    if (!this.transport.isConnected(serverId)) {
      return;
    }
    const generation = this.generations.get(serverId) ?? 0;
    const now = this.clock.now();
    // One message per worker at a time, in queue order: the next goes out
    // once the daemon has taken the one before it, so a refusal and its retry
    // can never let a later message overtake an earlier one.
    const busyWorkers = new Set<string>();
    for (const entry of [...this.entries]) {
      if (entry.serverId !== serverId || entry.state === "undeliverable") {
        continue;
      }
      if (busyWorkers.has(entry.workerId)) {
        continue;
      }
      if (entry.state === "uncertain") {
        // The daemon holds it; the ones behind it do not wait for its echo.
        continue;
      }
      busyWorkers.add(entry.workerId);
      if (this.inFlight.has(entry.id) || this.retryTimers.has(entry.id)) {
        continue;
      }
      if (now - entry.createdAt > OUTBOX_MAX_QUEUE_AGE_MS) {
        busyWorkers.delete(entry.workerId);
        this.settleUndeliverable(entry, "queue_expired");
        continue;
      }
      if (!this.send(entry, generation)) {
        return;
      }
    }
  }

  private send(entry: OutboxEntry, generation: number): boolean {
    let receipt: StructuredCommandReceipt;
    try {
      receipt = this.transport.sendInput(
        entry.serverId,
        entry.workerId,
        entry.text,
        {
          attachments: entry.attachments,
          conversationScopeKey: entry.conversationScopeKey,
          requestId: entry.id,
        },
      );
    } catch {
      // Not connected after all: it stays queued for the next connection.
      return false;
    }
    const stallTimer = this.clock.setTimer(() => {
      if (this.inFlight.get(entry.id)?.generation === generation) {
        this.transport.dropConnection(entry.serverId, "ack_stall");
      }
    }, OUTBOX_ACK_STALL_MS);
    this.inFlight.set(entry.id, { generation, stallTimer });
    const sentId = entry.id;
    void receipt.outcome.then((outcome) => {
      const flight = this.inFlight.get(sentId);
      if (flight?.generation === generation) {
        this.clock.clearTimer(flight.stallTimer);
        this.inFlight.delete(sentId);
      }
      this.settle(entry, sentId, outcome);
    });
    return true;
  }

  private settle(
    entry: OutboxEntry,
    sentId: string,
    outcome: StructuredCommandOutcome,
  ) {
    // The entry may have been discarded, resent or settled meanwhile.
    if (entry.id !== sentId || !this.entries.includes(entry)) {
      return;
    }
    switch (outcome.kind) {
      case "sent":
        this.settleAccepted(entry);
        return;
      case "pending":
        if (outcome.code === "input_uncertain") {
          this.settleUncertain(entry);
        } else {
          this.settleAccepted(entry);
        }
        return;
      case "connection_closed":
        // Lost the connection, not the message: it goes out again, under the
        // same id, on the next connection.
        return;
      case "failed":
        this.settleRefused(entry, outcome.failure.code);
        return;
    }
  }

  private settleAccepted(entry: OutboxEntry) {
    this.clearTimers(entry.id);
    this.entries = this.entries.filter((other) => other !== entry);
    this.persist();
    this.emit({ kind: "accepted", entry });
    this.emit({ kind: "changed", serverId: entry.serverId });
    this.scheduleDrain(entry.serverId);
  }

  private settleUncertain(entry: OutboxEntry) {
    entry.state = "uncertain";
    entry.uncertainSince = this.clock.now();
    this.persist();
    this.watchUncertain(entry, this.clock.now());
    this.emit({ kind: "uncertain", entry });
    this.scheduleDrain(entry.serverId);
  }

  private watchUncertain(entry: OutboxEntry, now: number) {
    const since = entry.uncertainSince ?? now;
    const remaining = Math.max(0, since + OUTBOX_UNCERTAIN_PATIENCE_MS - now);
    this.uncertainTimers.set(
      entry.id,
      this.clock.setTimer(() => {
        this.uncertainTimers.delete(entry.id);
        if (entry.state === "uncertain" && this.entries.includes(entry)) {
          this.settleUndeliverable(entry, "input_uncertain");
        }
      }, remaining),
    );
  }

  private settleRefused(entry: OutboxEntry, code: string) {
    if (code === STALE_RECEIPT_CODE) {
      // The daemon bound this id to a different payload and submitted
      // nothing: the same message continues under a fresh id.
      const previousId = entry.id;
      this.clearTimers(previousId);
      entry.id = this.newId();
      entry.attempts += 1;
      this.persist();
      this.emit({ kind: "reidentified", entry, previousId });
      if (entry.attempts >= OUTBOX_MAX_ATTEMPTS) {
        this.settleUndeliverable(entry, code);
        return;
      }
      this.scheduleDrain(entry.serverId);
      return;
    }
    entry.attempts += 1;
    entry.failureCode = code;
    if (entry.attempts >= OUTBOX_MAX_ATTEMPTS) {
      this.settleUndeliverable(entry, code);
      return;
    }
    this.persist();
    const wait =
      OUTBOX_RETRY_BACKOFF_MS[
        Math.min(entry.attempts - 1, OUTBOX_RETRY_BACKOFF_MS.length - 1)
      ];
    this.retryTimers.set(
      entry.id,
      this.clock.setTimer(() => {
        this.retryTimers.delete(entry.id);
        this.scheduleDrain(entry.serverId);
      }, wait),
    );
  }

  private settleUndeliverable(entry: OutboxEntry, code: string) {
    this.clearTimers(entry.id);
    entry.state = "undeliverable";
    entry.failureCode = code;
    delete entry.uncertainSince;
    this.persist();
    this.emit({ kind: "undeliverable", entry });
    this.emit({ kind: "changed", serverId: entry.serverId });
    this.scheduleDrain(entry.serverId);
  }

  private clearTimers(id: string) {
    for (const timers of [this.retryTimers, this.uncertainTimers]) {
      const handle = timers.get(id);
      if (handle !== undefined) {
        this.clock.clearTimer(handle);
        timers.delete(id);
      }
    }
    const flight = this.inFlight.get(id);
    if (flight) {
      this.clock.clearTimer(flight.stallTimer);
      this.inFlight.delete(id);
    }
  }

  private persist() {
    // Never write before the persisted queue has been read back: a save of
    // only the new entries would erase the rest.
    this.saveTail = this.saveTail
      .then(() => this.hydrate())
      .then(() => this.store.save(this.entries.map((entry) => ({ ...entry }))))
      .catch(() => undefined);
  }

  private emit(event: OutboxEvent) {
    for (const listener of [...this.listeners]) {
      try {
        listener(event);
      } catch {
        // A listener's failure never stops the queue.
      }
    }
  }
}
