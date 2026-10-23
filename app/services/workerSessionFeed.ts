/**
 * Follows the daemon's sequenced Session feed for one connection.
 *
 * The daemon sends the full `worker_session_list` once per connection (and
 * again only when asked) with the `seq` it is current as of. Every later
 * `worker_session_created|updated|archived` carries the next `seq`. The list
 * is a snapshot, never a receipt: nothing may be concluded from a Session
 * being absent from a later one.
 *
 * - A change at or before the last applied seq is a duplicate and is dropped.
 * - A change exactly one past it is applied.
 * - A change further ahead means one went missing: it is dropped and one
 *   resync (a new list request) is asked for.
 * - A change before the first list of the connection is dropped; the list is
 *   already on its way and is current as of a later seq.
 */
export type WorkerFeedVerdict = "apply" | "drop" | "resync";

export const WORKER_SESSION_FEED_TYPES = [
  "worker_session_list",
  "worker_session_created",
  "worker_session_updated",
  "worker_session_archived",
] as const;

export function isWorkerSessionFeedType(type: unknown): boolean {
  return (
    typeof type === "string" &&
    (WORKER_SESSION_FEED_TYPES as readonly string[]).includes(type)
  );
}

export class WorkerSessionFeed {
  private last: number | null = null;
  private resyncing = false;

  /** A new socket starts a new feed: the daemon sends its list first. */
  reset(): void {
    this.last = null;
    this.resyncing = false;
  }

  /** The resync request could not be sent; the next gap may ask again. */
  resyncNotSent(): void {
    this.resyncing = false;
  }

  accept(message: { type?: unknown; seq?: unknown }): WorkerFeedVerdict {
    const seq = message.seq;
    if (typeof seq !== "number" || !Number.isInteger(seq) || seq < 0) {
      // The daemon always stamps seq; a message without one cannot be placed.
      return "drop";
    }
    if (message.type === "worker_session_list") {
      this.last = seq;
      this.resyncing = false;
      return "apply";
    }
    if (this.last === null) {
      return "drop";
    }
    if (seq <= this.last) {
      return "drop";
    }
    if (seq === this.last + 1) {
      this.last = seq;
      return "apply";
    }
    if (this.resyncing) {
      return "drop";
    }
    this.resyncing = true;
    return "resync";
  }
}
