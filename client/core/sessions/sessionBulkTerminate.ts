/**
 * Batch Session termination against the daemon's authoritative terminate
 * lifecycle (`kill_worker` → route-aware teardown → watcher removal).
 *
 * Contract audited from daemon/server/server.go and daemon/modelprofiles:
 * - Command: `kill_worker` with `worker_id` and optional `request_id`.
 * - Failure: daemon replies `{type:"error", code, message, request_id, ...}`.
 * - Success: the daemon replies `{type:"worker_killed", request_id, worker_id}`
 *   once the Session is confirmed gone. The sequenced `worker_session_archived`
 *   change is the same fact on the list. Absence from a list proves nothing.
 *
 * The batch never deletes locally and never invents state: entries are a
 * point-in-time snapshot of stable Session IDs; every entry settles from
 * daemon-owned evidence or a retryable timeout, and a settled entry never
 * changes again (first-wins), so concurrent disappearance and duplicate
 * events cannot corrupt the outcome.
 *
 * From the Expo app's services/sessionBulkTerminate.ts. There the batch sent
 * `kill_worker` itself and matched replies by request_id and serverId; here it
 * asks the current computer's Transport, which correlates the reply, so one
 * batch is one computer's Sessions. Two consequences:
 * - a disconnect fails the waiting entries at once with the offline message
 *   (the Expo batch waited for the timeout);
 * - an `error` reply without a message reads "kill_worker failed" (the
 *   Expo batch showed the timeout message).
 */
import type { Frame } from "../transport";
export type SessionTerminationStatus = "pending" | "succeeded" | "failed";

export interface SessionTerminationEntry {
  /** Canonical stable key: makeSessionKey(serverId, workerId). */
  sessionKey: string;
  serverId: string;
  workerId: string;
  status: SessionTerminationStatus;
  /** Truthful failure detail for the retryable summary. */
  error?: string;
}

export interface SessionTerminationSummary {
  total: number;
  succeeded: number;
  failed: number;
  pending: number;
  running: boolean;
  /** Only settled failures, in submission order. */
  failedEntries: SessionTerminationEntry[];
}

/**
 * The part of the current computer's Transport (core/transport.ts) the batch
 * uses: `request` sends `{type, request_id, ...body}` and settles on the
 * correlated reply, a correlated `error`, the timeout or a disconnect.
 */
export interface SessionTerminationTransport {
  request(
    type: string,
    body: Record<string, unknown>,
    options: { reply: string; errors?: string[]; timeoutMs?: number },
  ): Promise<Frame>;
  on(type: string, fn: (frame: Frame) => void): () => void;
}

export interface SessionTerminationBatchOptions {
  transport: SessionTerminationTransport;
  /** Snapshot of stable Session IDs to terminate (must be non-empty). */
  entries: SessionTerminationEntry[];
  /** Called on every settle with the current summary; final call has running=false. */
  onSettled(summary: SessionTerminationSummary): void;
  /** Per-entry acknowledgement timeout; default 20s. */
  timeoutMs?: number;
}

export interface SessionTerminationTarget {
  sessionKey: string;
  serverId: string;
  workerId: string;
}

/** Build the pending entry snapshot for a set of stable Session targets. */
export function createSessionTerminationEntries(
  targets: readonly SessionTerminationTarget[],
): SessionTerminationEntry[] {
  return targets.map((target) => ({
    sessionKey: target.sessionKey,
    serverId: target.serverId,
    workerId: target.workerId,
    status: "pending" as const,
  }));
}

export function summarizeSessionTermination(
  entries: readonly SessionTerminationEntry[],
): SessionTerminationSummary {
  let succeeded = 0;
  let failed = 0;
  let pending = 0;
  const failedEntries: SessionTerminationEntry[] = [];
  for (const entry of entries) {
    switch (entry.status) {
      case "succeeded":
        succeeded += 1;
        break;
      case "failed":
        failed += 1;
        failedEntries.push(entry);
        break;
      default:
        pending += 1;
        break;
    }
  }
  return {
    total: entries.length,
    succeeded,
    failed,
    pending,
    running: pending > 0,
    failedEntries,
  };
}

export const SESSION_TERMINATION_TIMEOUT_MS = 20000;
export const SESSION_TERMINATION_TIMEOUT_MESSAGE =
  "The daemon did not acknowledge termination in time. Retry to terminate this session.";
export const SESSION_TERMINATION_OFFLINE_MESSAGE =
  "The daemon is not connected. Reconnect and retry to terminate this session.";

/** What the Transport rejects with when it is not connected, or the socket drops while waiting. */
const NOT_CONNECTED = "Daemon is not connected.";

/** The batch's own timer settles an entry; the request outlives it so its rejection never wins. */
const REQUEST_TIMEOUT_SLACK_MS = 1000;

export function sessionTerminationConfirmMessage(
  count: number,
): string {
  const target = count === 1 ? "This session" : `These ${count} sessions`;
  return `${target} will be terminated. Sessions leave the list once the daemon confirms termination.`;
}

export function sessionTerminationSummaryMessage(
  summary: SessionTerminationSummary,
  failedTitles: readonly string[],
): string {
  const failedCount = summary.failed;
  if (failedCount === 0) {
    return "";
  }
  const failedLabel =
    failedCount === 1 ? "1 session" : `${failedCount} sessions`;
  const lead =
    summary.succeeded === 0
      ? `Could not terminate ${failedLabel}.`
      : `${summary.succeeded} of ${summary.total} terminated; ${failedLabel} failed.`;
  const names = failedTitles.filter(Boolean);
  const detail =
    names.length > 0
      ? `\n\n${names.join(", ")}`
      : "";
  return `${lead}${detail}\n\nThese sessions remain selected. Retry to terminate them.`;
}

function archivedWorkerId(payload: Frame): string | null {
  const id = (payload?.worker_session as { id?: unknown } | undefined)?.id;
  return typeof id === "string" && id ? id : null;
}

/**
 * Runs one confirmed batch. `start()` submits exactly once (a second call is
 * a no-op), subscribes to daemon events, and settles each entry from
 * authoritative evidence:
 * - `error` matching this entry's request → failed (daemon message kept).
 * - `worker_killed` answering this entry's request → succeeded.
 * - `worker_session_archived` for the Session → succeeded.
 * - not connected, or disconnected while waiting → failed (retryable).
 * - per-entry timeout → failed (retryable).
 */
export class SessionTerminationBatch {
  private readonly entries: SessionTerminationEntry[];
  private readonly transport: SessionTerminationTransport;
  private readonly onSettled: (summary: SessionTerminationSummary) => void;
  private readonly timeoutMs: number;
  private readonly timers = new Map<string, ReturnType<typeof setTimeout>>();
  private readonly stops: Array<() => void> = [];
  private started = false;
  private disposed = false;

  constructor(options: SessionTerminationBatchOptions) {
    this.entries = createSessionTerminationEntries(options.entries);
    this.transport = options.transport;
    this.onSettled = options.onSettled;
    this.timeoutMs = options.timeoutMs ?? SESSION_TERMINATION_TIMEOUT_MS;
  }

  get summary(): SessionTerminationSummary {
    return summarizeSessionTermination(this.entries);
  }

  get isRunning(): boolean {
    return this.started && !this.disposed && this.summary.pending > 0;
  }

  /**
   * Submit the batch. Returns false (without submitting) when already
   * started, preventing duplicate submissions while a batch is running.
   */
  start(): boolean {
    if (this.started) {
      return false;
    }
    this.started = true;
    this.subscribe();

    for (const entry of this.entries) {
      if (entry.status !== "pending") {
        continue;
      }
      const sessionKey = entry.sessionKey;
      this.transport
        .request(
          "kill_worker",
          { worker_id: entry.workerId },
          { reply: "worker_killed", timeoutMs: this.timeoutMs + REQUEST_TIMEOUT_SLACK_MS },
        )
        .then(
          () => this.settle(sessionKey, "succeeded"),
          (error: unknown) => {
            const message = error instanceof Error ? error.message : "";
            this.settle(
              sessionKey,
              "failed",
              !message || message === NOT_CONNECTED
                ? SESSION_TERMINATION_OFFLINE_MESSAGE
                : message,
            );
          },
        );
      this.timers.set(
        sessionKey,
        setTimeout(() => {
          this.timers.delete(sessionKey);
          this.settle(
            sessionKey,
            "failed",
            SESSION_TERMINATION_TIMEOUT_MESSAGE,
          );
        }, this.timeoutMs),
      );
    }
    this.emitSummary();
    return true;
  }

  dispose(): void {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    for (const timer of this.timers.values()) {
      clearTimeout(timer);
    }
    this.timers.clear();
    for (const stop of this.stops) {
      stop();
    }
    this.stops.length = 0;
  }

  private settle(
    sessionKey: string,
    status: SessionTerminationStatus,
    error?: string,
  ): void {
    if (this.disposed) {
      return;
    }
    const entry = this.entries.find(
      (candidate) =>
        candidate.sessionKey === sessionKey &&
        candidate.status === "pending",
    );
    if (!entry) {
      // Already settled: first-wins, so concurrent disappearance after a
      // failure reply (or vice versa) can never flip an outcome.
      return;
    }
    entry.status = status;
    if (error) {
      entry.error = error;
    }
    const timer = this.timers.get(sessionKey);
    if (timer) {
      clearTimeout(timer);
      this.timers.delete(sessionKey);
    }
    this.emitSummary();
  }

  private emitSummary(): void {
    this.onSettled(this.summary);
  }

  private subscribe(): void {
    // The request answers the entry it sent; the archived change on the list
    // is the same fact, and may arrive first.
    this.stops.push(
      this.transport.on("worker_session_archived", (frame) => {
        const workerId = archivedWorkerId(frame);
        if (workerId == null) {
          return;
        }
        for (const entry of this.entries) {
          if (entry.status === "pending" && entry.workerId === workerId) {
            this.settle(entry.sessionKey, "succeeded");
          }
        }
      }),
    );
  }
}
