import { describe, expect, test } from "bun:test";
import { catCanPlay, DOUBLE_TAP_MS, isDoubleTap, tossOffset, tossTilt } from "./catPlay";

describe("cat play", () => {
  test("the empty bed has no cat to play with", () => {
    expect(catCanPlay("homeless")).toBe(false);
    expect(catCanPlay("idle")).toBe(true);
    expect(catCanPlay("attention")).toBe(true);
  });

  test("a second tap close behind is a double-tap", () => {
    expect(isDoubleTap(null, 1000)).toBe(false);
    expect(isDoubleTap(1000, 1000 + DOUBLE_TAP_MS)).toBe(true);
    expect(isDoubleTap(1000, 1001 + DOUBLE_TAP_MS)).toBe(false);
  });

  test("the toss follows the finger and stops within reach", () => {
    expect(tossOffset(0, 30)).toBe(0);
    expect(tossOffset(3, 30)).toBeCloseTo(3, 0);
    expect(tossOffset(400, 30)).toBeLessThan(30);
    expect(tossOffset(-400, 30)).toBeGreaterThan(-30);
    expect(tossOffset(10, 0)).toBe(0);
  });

  test("the held cat tilts a little with the drag", () => {
    expect(tossTilt(0)).toBe(0);
    expect(tossTilt(5000)).toBe(12);
    expect(tossTilt(-5000)).toBe(-12);
  });
});
