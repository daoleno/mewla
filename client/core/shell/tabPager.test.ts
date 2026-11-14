import { describe, expect, test } from "bun:test";
import { indicatorStops, pagerProgress, settlePage } from "./tabPager";

const W = 390;
const at = (from: number, offset: number, velocity = 0, count = 2, rest = from) => settlePage({ rest, from, count, width: W, offset, velocity });

describe("a swipe between tabs settles like a pager", () => {
  test("past half a page goes on; less goes back", () => {
    expect(at(0, -W * 0.6)).toBe(1);
    expect(at(0, -W * 0.4)).toBe(0);
    expect(at(1, W * 0.6)).toBe(0);
    expect(at(1, W * 0.3)).toBe(1);
  });

  test("a flick turns the page even when short", () => {
    expect(at(0, -60, -1500)).toBe(1);
    expect(at(1, 60, 1500)).toBe(0);
    // A flick back against the drag returns.
    expect(at(0, -W * 0.6, 2500)).toBe(0);
  });

  test("never past the first or the last page, and one page at most per swipe", () => {
    expect(at(0, W * 0.9, 3000)).toBe(0);
    expect(at(1, -W * 0.9, -3000)).toBe(1);
    expect(at(0, -W * 2.5, -4000, 4)).toBe(1);
  });

  test("a swipe that interrupted a settle counts from the page it was going to", () => {
    // Laid out on 0, springing to 1, caught at 70 % of the way: a small push on is still 1, not 2.
    expect(settlePage({ rest: 0, from: 1, count: 3, width: W, offset: -W * 0.7, velocity: -200 })).toBe(1);
  });

  test("one page, or no width yet, stays", () => {
    expect(at(0, -W, -3000, 1)).toBe(0);
    expect(settlePage({ rest: 1, from: 1, count: 2, width: 0, offset: -100, velocity: -900 })).toBe(1);
  });
});

describe("the chosen segment follows the row", () => {
  test("progress is the page index under the finger", () => {
    expect(pagerProgress(0, 0, W)).toBe(0);
    expect(pagerProgress(0, -W / 2, W)).toBe(0.5);
    expect(pagerProgress(1, W / 4, W)).toBe(0.75);
    expect(pagerProgress(1, 50, 0)).toBe(1);
  });

  test("its mark covers each segment exactly, whatever their widths", () => {
    const segments = [{ x: 3, width: 60 }, { x: 63, width: 70 }];
    const stops = indicatorStops(segments, 60);
    segments.forEach((s, i) => {
      // A `base` wide shape at 0, moved and stretched about its centre.
      const left = stops.translate[i]! + 30 - (60 * stops.scale[i]!) / 2;
      expect(left).toBeCloseTo(s.x);
      expect(60 * stops.scale[i]!).toBeCloseTo(s.width);
    });
  });
});
