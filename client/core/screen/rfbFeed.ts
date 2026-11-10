// Where the daemon's RFB bytes go while the viewer starts its noVNC client.
//
// The daemon speaks first: its ProtocolVersion arrives right behind the socket opening, often in
// the same batch as the host's "open". The client starts a moment after "open" (it waits for the
// H.264 probe), and bytes that arrive before it exists must wait for it, not vanish: a dropped
// ProtocolVersion leaves both ends waiting for the other for good.

export type RfbFeedState = "idle" | "opening" | "open";

export interface RfbFeed {
  readonly state: RfbFeedState;
  /** A session was asked for but its client does not exist yet: bytes wait. */
  opening(): void;
  /** The client exists; returns what waited for it, in order. */
  opened(): string[];
  /** The session ended: nothing waits, later bytes are not for anyone. */
  closed(): void;
  /** Bytes from the daemon: what may go to the client now (empty while it waits or none exists). */
  accept(chunks: string[]): string[];
}

export function createRfbFeed(): RfbFeed {
  let state: RfbFeedState = "idle";
  let held: string[] = [];
  return {
    get state() { return state; },
    opening() { state = "opening"; held = []; },
    opened() {
      const waiting = held;
      held = [];
      state = "open";
      return waiting;
    },
    closed() { state = "idle"; held = []; },
    accept(chunks) {
      if (state === "open") return chunks;
      if (state === "opening") held.push(...chunks);
      return [];
    },
  };
}
