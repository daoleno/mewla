import { describe, expect, test } from "bun:test";
import { LIVE_DEADLINE_MS, startLiveDeadline, type DeadlineTimers } from "./screenLiveDeadline";

function fakeTimers() {
  const pending: { callback: () => void; ms: number; cleared: boolean }[] = [];
  const timers: DeadlineTimers = {
    set(callback, ms) { const entry = { callback, ms, cleared: false }; pending.push(entry); return entry; },
    clear(handle) { (handle as { cleared: boolean }).cleared = true; },
  };
  const fire = () => pending.filter((p) => !p.cleared).forEach((p) => p.callback());
  return { timers, pending, fire };
}

describe("live deadline", () => {
  test("a connection that never goes live is expired once, after the default wait", () => {
    const { timers, pending, fire } = fakeTimers();
    let expired = 0;
    startLiveDeadline(() => expired++, undefined, timers);
    expect(pending[0].ms).toBe(LIVE_DEADLINE_MS);
    fire();
    fire();
    expect(expired).toBe(1);
  });

  test("going live in time cancels it", () => {
    const { timers, fire } = fakeTimers();
    let expired = 0;
    const cancel = startLiveDeadline(() => expired++, 5000, timers);
    cancel();
    fire();
    expect(expired).toBe(0);
  });

  test("cancelling after it expired is harmless", () => {
    const { timers, fire } = fakeTimers();
    let expired = 0;
    const cancel = startLiveDeadline(() => expired++, 5000, timers);
    fire();
    cancel();
    expect(expired).toBe(1);
  });
});
