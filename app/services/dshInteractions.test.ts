import { describe, expect, test } from "bun:test";
import { nextDSHPollDelayMs } from "./dshInteractions";

describe("nextDSHPollDelayMs", () => {
  test("a reply with a revision is followed at once because the daemon holds the next request", () => {
    expect(nextDSHPollDelayMs({ ok: true, rev: 4 })).toBe(0);
    expect(nextDSHPollDelayMs({ ok: true, rev: 0 })).toBe(0);
  });

  test("a daemon that does not hold requests is asked at the fixed pace", () => {
    expect(nextDSHPollDelayMs({ ok: true })).toBe(1200);
  });

  test("failures back off and stop at the cap", () => {
    const delays = [1, 2, 3, 4, 5, 6, 20].map((failures) => nextDSHPollDelayMs({ ok: false, failures }));
    expect(delays).toEqual([1200, 2400, 4800, 9600, 19200, 30000, 30000]);
  });
});
