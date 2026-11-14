import { describe, expect, test } from "bun:test";
import { ANSWER_MS, ClaudeWheel, FRAME_MS, LinePlanner, MAX_OWED, RETRY_MS, WheelGate, type GateClock } from "./wheelGate";

/** Rows Claude moves for presses at the given times (ms), one direction. */
function rows(times: number[], dir = 1): number {
  const wheel = new ClaudeWheel();
  return times.reduce((sum, t) => sum + wheel.press(dir, t), 0);
}

describe("Claude Code's wheel, as measured against Claude Code 2.1.323 in its fullscreen view", () => {
  test("presses in one write accelerate: 3 move 3 rows, 10 move 19", () => {
    expect(rows([1000, 1000, 1000])).toBe(3);
    expect(rows(Array(10).fill(1000))).toBe(19);
  });

  test("single presses 16 ms apart accelerate (12 move about twice as far); 50 ms apart move a row each", () => {
    expect(rows(Array.from({ length: 12 }, (_, i) => 1000 + i * 16))).toBeGreaterThanOrEqual(24);
    expect(rows(Array.from({ length: 12 }, (_, i) => 1000 + i * 50))).toBe(12);
  });

  test("the first press against the last direction moves nothing; a bounce back turns on the fast wheel mode", () => {
    const wheel = new ClaudeWheel();
    expect(wheel.press(1, 1000)).toBe(1);
    expect(wheel.press(-1, 1100)).toBe(0);
    expect(wheel.press(-1, 1150)).toBe(1);
    const bounce = new ClaudeWheel();
    bounce.press(1, 1000);
    bounce.press(-1, 1100);
    bounce.press(1, 1150);
    expect(bounce.press(1, 1200)).toBeGreaterThan(3);
  });

  test("a plan never moves Claude past the travel, and counts the press a reversal loses", () => {
    const wheel = new ClaudeWheel();
    expect(wheel.plan(10, 1000)).toEqual({ presses: 7, lines: 10 });
    // Right after, one press would move two rows: one line waits.
    expect(wheel.plan(1, 1001)).toEqual({ presses: 0, lines: 0 });
    expect(wheel.plan(1, 1060)).toEqual({ presses: 1, lines: 1 });
    // Back toward now: the first press is lost to the reversal, so two go for one row.
    expect(wheel.plan(-1, 1200)).toEqual({ presses: -2, lines: -1 });
  });
});

function fakeClock() {
  let now = 1000;
  let timers: Array<{ at: number; fn: () => void; id: number }> = [];
  let ids = 0;
  const clock: GateClock = {
    now: () => now,
    setTimeout: (fn, ms) => {
      const id = ++ids;
      timers.push({ at: now + ms, fn, id });
      return id;
    },
    clearTimeout: (id) => {
      timers = timers.filter((t) => t.id !== id);
    },
  };
  const advance = (ms: number) => {
    const end = now + ms;
    for (;;) {
      const next = timers.filter((t) => t.at <= end).sort((a, b) => a.at - b.at)[0];
      if (!next) break;
      timers = timers.filter((t) => t !== next);
      now = next.at;
      next.fn();
    }
    now = end;
  };
  return { clock, advance };
}

describe("the wheel gate", () => {
  test("one write in flight: the travel made meanwhile goes as one write when the program answers", () => {
    const { clock, advance } = fakeClock();
    const sent: number[] = [];
    const gate = new WheelGate((n) => sent.push(n), new LinePlanner(), clock);
    gate.add(1);
    gate.add(1);
    gate.add(2);
    expect(sent).toEqual([1]);
    advance(20);
    gate.answered();
    expect(sent).toEqual([1, 3]);
  });

  test("at most one write a frame, however fast the program answers", () => {
    const { clock, advance } = fakeClock();
    const sent: number[] = [];
    const gate = new WheelGate((n) => sent.push(n), new LinePlanner(), clock);
    gate.add(1);
    gate.answered();
    gate.add(1);
    expect(sent).toEqual([1]);
    advance(FRAME_MS);
    expect(sent).toEqual([1, 1]);
  });

  test("a program that does not answer (nothing left to scroll) holds the next write only so long", () => {
    const { clock, advance } = fakeClock();
    const sent: number[] = [];
    const gate = new WheelGate((n) => sent.push(n), new LinePlanner(), clock);
    gate.add(2);
    gate.add(1);
    advance(ANSWER_MS - 1);
    expect(sent).toEqual([2]);
    advance(1);
    expect(sent).toEqual([2, 1]);
  });

  test("a reversal nets out, a touch drops what waits, and a flood past MAX_OWED is cut", () => {
    const { clock, advance } = fakeClock();
    const sent: number[] = [];
    const gate = new WheelGate((n) => sent.push(n), new LinePlanner(), clock);
    gate.add(1);
    gate.add(3);
    gate.add(-5);
    advance(20);
    gate.answered();
    expect(sent).toEqual([1, -2]);
    gate.add(4);
    gate.stop();
    advance(20);
    gate.answered();
    advance(ANSWER_MS);
    expect(sent).toEqual([1, -2]);
    gate.add(1000);
    expect(sent.at(-1)).toBe(30);
    expect(gate.waiting).toBe(MAX_OWED - 30);
  });

  test("travel Claude cannot take now (it would overshoot) is tried again past its window", () => {
    const { clock, advance } = fakeClock();
    const sent: number[] = [];
    const gate = new WheelGate((n) => sent.push(n), new ClaudeWheel(), clock);
    gate.add(10);
    expect(sent).toEqual([7]);
    advance(FRAME_MS);
    gate.answered();
    gate.add(1);
    expect(sent).toEqual([7]);
    advance(RETRY_MS);
    expect(sent).toEqual([7, 1]);
    expect(gate.waiting).toBe(0);
  });
});
