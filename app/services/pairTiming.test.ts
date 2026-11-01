import { describe, expect, test } from "bun:test";
import {
  abandonPairTimeline,
  bindPairTimeline,
  finishPairTimeline,
  markPairTimeline,
  PairTimeline,
  slowPairingToast,
  startPairTimeline,
} from "./pairTiming";

function clock(start = 0) {
  let t = start;
  return { now: () => t, advance: (ms: number) => (t += ms) };
}

describe("PairTimeline", () => {
  test("each mark closes the hop since the previous one", () => {
    const c = clock();
    const timeline = new PairTimeline(c.now);
    c.advance(5);
    timeline.mark("parse");
    c.advance(70_000);
    timeline.mark("tunnel");
    expect(timeline.segments()).toEqual([
      { name: "parse", ms: 5 },
      { name: "tunnel", ms: 70_000 },
    ]);
    expect(timeline.slowest()).toEqual({ name: "tunnel", ms: 70_000 });
    expect(timeline.summary()).toBe("parse 5ms · tunnel 70.0s");
  });

  test("the confirm prompt waits on a person and is never counted or blamed", () => {
    const c = clock();
    const timeline = new PairTimeline(c.now);
    c.advance(30_000);
    timeline.mark("confirm");
    c.advance(400);
    timeline.mark("pair");
    expect(timeline.workMs()).toBe(400);
    expect(timeline.slowest()).toEqual({ name: "pair", ms: 400 });
    expect(timeline.isSlow()).toBe(false);
  });

  test("a pairing past the threshold is slow and is slow", () => {
    const c = clock();
    const timeline = new PairTimeline(c.now);
    c.advance(2_000);
    timeline.mark("tunnel");
    c.advance(9_000);
    timeline.mark("pair-request");
    expect(timeline.isSlow()).toBe(true);
  });
});

describe("active pairing", () => {
  test("only the imported computer's first connection closes it, once", () => {
    const c = clock();
    startPairTimeline(c.now);
    c.advance(10);
    markPairTimeline("parse");
    bindPairTimeline("srv-a");
    c.advance(12_000);
    expect(finishPairTimeline("srv-other")).toBeNull();
    const report = finishPairTimeline("srv-a");
    expect(report?.slow).toBe(true);
    expect(report?.slowest).toEqual({ name: "connected", ms: 12_000 });
    expect(slowPairingToast(report!).title).toBe("Pairing took 12.0s");
    expect(finishPairTimeline("srv-a")).toBeNull();
  });

  test("a computer connecting long after the pairing is not reported as pairing", () => {
    const c = clock();
    startPairTimeline(c.now);
    bindPairTimeline("srv-b");
    c.advance(6 * 60_000);
    expect(finishPairTimeline("srv-b")).toBeNull();
  });

  test("a failed pairing reports where it stopped and leaves nothing to close", () => {
    const c = clock();
    startPairTimeline(c.now);
    c.advance(45_000);
    const report = abandonPairTimeline("failed");
    expect(report?.slowest).toEqual({ name: "failed", ms: 45_000 });
    bindPairTimeline("srv-c");
    expect(finishPairTimeline("srv-c")).toBeNull();
  });
});
