import { describe, expect, test } from "bun:test";
import { LOADING_VEIL, loadingVeilPhase } from "./loadingVeil";

const { delayMs, minMs, fadeMs } = LOADING_VEIL;

describe("loadingVeilPhase", () => {
  test("waits before showing anything", () => {
    expect(loadingVeilPhase({ startedAt: 0, endedAt: null, now: 100 })).toEqual({ phase: "waiting", nextAt: delayMs });
  });

  test("a fast load never shows the loading screen", () => {
    expect(loadingVeilPhase({ startedAt: 0, endedAt: 200, now: 200 })).toEqual({ phase: "gone" });
    expect(loadingVeilPhase({ startedAt: 0, endedAt: 200, now: 5000 })).toEqual({ phase: "gone" });
  });

  test("shows while loading after the delay", () => {
    expect(loadingVeilPhase({ startedAt: 0, endedAt: null, now: delayMs })).toEqual({ phase: "shown" });
    expect(loadingVeilPhase({ startedAt: 0, endedAt: null, now: 60_000 })).toEqual({ phase: "shown" });
  });

  test("a load ending just after it showed keeps it up for the minimum", () => {
    const endedAt = delayMs + 50;
    const leaveAt = delayMs + minMs;
    expect(loadingVeilPhase({ startedAt: 0, endedAt, now: endedAt })).toEqual({ phase: "shown", nextAt: leaveAt });
    expect(loadingVeilPhase({ startedAt: 0, endedAt, now: leaveAt })).toEqual({ phase: "leaving", nextAt: leaveAt + fadeMs });
    expect(loadingVeilPhase({ startedAt: 0, endedAt, now: leaveAt + fadeMs })).toEqual({ phase: "gone" });
  });

  test("a long load fades out as soon as it ends", () => {
    expect(loadingVeilPhase({ startedAt: 0, endedAt: 2000, now: 2000 })).toEqual({ phase: "leaving", nextAt: 2000 + fadeMs });
    expect(loadingVeilPhase({ startedAt: 0, endedAt: 2000, now: 2000 + fadeMs })).toEqual({ phase: "gone" });
  });
});
