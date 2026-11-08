/**
 * How long a screen connection may stay "connecting" before it is called lost, so a viewer that
 * never completes its handshake (or a socket that never opens) ends in Reconnect, not in a spinner.
 * A picture normally arrives within a second or two, over a relay a few more.
 */
export const LIVE_DEADLINE_MS = 20_000;
export const LIVE_DEADLINE_REASON = "The screen did not answer. Try again.";

export interface DeadlineTimers {
  set(callback: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

const realTimers: DeadlineTimers = {
  set: (callback, ms) => setTimeout(callback, ms),
  clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
};

/** Calls `expire` after `ms` unless the returned function is called first. */
export function startLiveDeadline(expire: () => void, ms: number = LIVE_DEADLINE_MS, timers: DeadlineTimers = realTimers): () => void {
  let done = false;
  const handle = timers.set(() => {
    if (done) return;
    done = true;
    expire();
  }, ms);
  return () => {
    if (done) return;
    done = true;
    timers.clear(handle);
  };
}
