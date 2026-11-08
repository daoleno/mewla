import { describe, expect, test } from "bun:test";
import {
  DEFAULT_FLOAT_CORNER,
  FLOAT_DEFAULT,
  FLOAT_LARGE,
  FLOAT_WIDE,
  FLOAT_WIDE_LARGE,
  clampFloatFraction,
  floatOrigin,
  floatSize,
  nearestFloatCorner,
  parseFloatCorner,
  parseFloatFraction,
  resizedFloatWidth,
  toggledFloatFraction,
} from "./floatGeometry";

describe("float geometry", () => {
  const area = { left: 0, top: 100, right: 400, bottom: 700 };
  const size = { width: 80, height: 180 };

  test("sits in the four corners of the free area", () => {
    expect(floatOrigin("top-left", area, size, 12)).toEqual({ x: 12, y: 112 });
    expect(floatOrigin("top-right", area, size, 12)).toEqual({ x: 308, y: 112 });
    expect(floatOrigin("bottom-left", area, size, 12)).toEqual({ x: 12, y: 508 });
    expect(floatOrigin("bottom-right", area, size, 12)).toEqual({ x: 308, y: 508 });
  });

  test("a keyboard that shrinks the area moves the bottom corners up and never off the top", () => {
    const small = { ...area, bottom: 200 };
    expect(floatOrigin("bottom-right", small, size, 12).y).toBe(100);
    expect(floatOrigin("top-right", small, size, 12).y).toBe(112);
  });

  test("snaps to the corner nearest the float's centre", () => {
    expect(nearestFloatCorner({ x: 50, y: 150 }, area)).toBe("top-left");
    expect(nearestFloatCorner({ x: 350, y: 150 }, area)).toBe("top-right");
    expect(nearestFloatCorner({ x: 50, y: 650 }, area)).toBe("bottom-left");
    expect(nearestFloatCorner({ x: 350, y: 650 }, area)).toBe("bottom-right");
  });

  const window = { width: 411, height: 914 };
  const ratio = 720 / 1600;

  test("is the agent display's exact shape, at 55% of the width by default", () => {
    const size = floatSize(ratio, FLOAT_DEFAULT, window);
    expect(size.width).toBeCloseTo(226.05, 2);
    expect(size.width / size.height).toBeCloseTo(ratio, 10);
    // 502dp tall: under 60% of the window's height.
    expect(size.height).toBeLessThan(0.6 * window.height);
  });

  test("the default never covers more than 60% of the height, even on a short wide display", () => {
    const wide = floatSize(0.8, FLOAT_DEFAULT, { width: 411, height: 500 });
    expect(wide.height).toBeLessThanOrEqual(0.6 * 500 + 1e-9);
    expect(wide.width / wide.height).toBeCloseTo(0.8, 10);
  });

  test("a size the person chose may be taller, up to 85% of the height", () => {
    const large = floatSize(ratio, FLOAT_LARGE, window);
    expect(large.width).toBeCloseTo(328.8, 1);
    expect(large.height).toBeCloseTo(730.7, 1);
    expect(large.height).toBeLessThanOrEqual(0.85 * window.height);
    expect(floatSize(ratio, 0.9, { width: 411, height: 700 }).height).toBeLessThanOrEqual(0.85 * 700 + 1e-9);
  });

  test("the least width is 30% of the window, however little room there is", () => {
    const tight = floatSize(ratio, FLOAT_DEFAULT, window, 100);
    expect(tight.width).toBeCloseTo(0.3 * window.width, 5);
    expect(tight.width / tight.height).toBeCloseTo(ratio, 10);
  });

  test("a keyboard gives height first and keeps the shape", () => {
    const size = floatSize(ratio, FLOAT_DEFAULT, window, 400);
    expect(size.height).toBeCloseTo(400, 5);
    expect(size.width / size.height).toBeCloseTo(ratio, 10);
  });

  test("a display with no size yet falls back to a phone's shape", () => {
    expect(floatSize(0, FLOAT_DEFAULT, window).width / floatSize(0, FLOAT_DEFAULT, window).height).toBeCloseTo(9 / 20, 10);
  });

  test("dragging the inner corner outward grows it along the diagonal, for every corner", () => {
    // Anchored top-left: the handle is bottom-right, outward is +x +y.
    expect(resizedFloatWidth(200, ratio, { x: 0, y: 0 }, "top-left")).toBe(200);
    expect(resizedFloatWidth(200, ratio, { x: 30, y: 0 }, "top-left")).toBeGreaterThan(200);
    expect(resizedFloatWidth(200, ratio, { x: 0, y: 40 }, "top-left")).toBeGreaterThan(200);
    expect(resizedFloatWidth(200, ratio, { x: -30, y: -40 }, "top-left")).toBeLessThan(200);
    // Anchored top-right: the handle is bottom-left, outward is -x +y.
    expect(resizedFloatWidth(200, ratio, { x: -30, y: 0 }, "top-right")).toBeGreaterThan(200);
    expect(resizedFloatWidth(200, ratio, { x: 30, y: 0 }, "top-right")).toBeLessThan(200);
    // Anchored bottom-right: the handle is top-left, outward is -x -y.
    expect(resizedFloatWidth(200, ratio, { x: 0, y: -40 }, "bottom-right")).toBeGreaterThan(200);
    // Anchored bottom-left: the handle is top-right, outward is +x -y.
    expect(resizedFloatWidth(200, ratio, { x: 30, y: -40 }, "bottom-left")).toBeGreaterThan(200);
  });

  test("a drag along the diagonal moves the corner exactly as far as the width says", () => {
    const start = 200;
    const height = start / ratio;
    // The handle moves by (dw, dw / ratio) when the width grows by dw.
    expect(resizedFloatWidth(start, ratio, { x: 20, y: 20 / ratio }, "top-left")).toBeCloseTo(start + 20, 8);
    expect(height).toBeGreaterThan(start);
  });

  test("a double tap goes between the default and the large size", () => {
    expect(toggledFloatFraction(FLOAT_DEFAULT)).toBe(FLOAT_LARGE);
    expect(toggledFloatFraction(FLOAT_LARGE)).toBe(FLOAT_DEFAULT);
    expect(toggledFloatFraction(0.4)).toBe(FLOAT_DEFAULT);
  });

  test("remembers a size only in range, and clamps one to 30%-90%", () => {
    expect(parseFloatFraction("0.7")).toBe(0.7);
    expect(parseFloatFraction("0.1")).toBe(FLOAT_DEFAULT);
    expect(parseFloatFraction("2")).toBe(FLOAT_DEFAULT);
    expect(parseFloatFraction("wide")).toBe(FLOAT_DEFAULT);
    expect(parseFloatFraction(null)).toBe(FLOAT_DEFAULT);
    expect(clampFloatFraction(0.1)).toBe(0.3);
    expect(clampFloatFraction(1.4)).toBe(0.9);
    expect(clampFloatFraction(0.5)).toBe(0.5);
  });

  test("remembers only a corner it knows", () => {
    expect(parseFloatCorner("bottom-left")).toBe("bottom-left");
    expect(parseFloatCorner("middle")).toBe("bottom-right");
    expect(parseFloatCorner(null)).toBe("bottom-right");
  });

  test("starts in the bottom-right corner, above the composer, so most of the chat stays readable", () => {
    expect(DEFAULT_FLOAT_CORNER).toBe("bottom-right");
    const area = { left: 0, top: 100, right: 400, bottom: 700 };
    const size = { width: 220, height: 480 };
    expect(floatOrigin(DEFAULT_FLOAT_CORNER, area, size, 12)).toEqual({ x: 168, y: 208 });
  });

  test("on a wide window the default and large sizes stay corner windows; a dragged size is not capped", () => {
    const wide = { width: 1400, height: 900 };
    expect(floatSize(1.6, FLOAT_DEFAULT, wide).width).toBe(FLOAT_WIDE);
    expect(floatSize(1.6, FLOAT_LARGE, wide).width).toBe(FLOAT_WIDE_LARGE);
    expect(floatSize(1.6, 0.5, wide).width).toBeCloseTo(700, 5);
    expect(floatSize(1.6, 0.5, wide).width / floatSize(1.6, 0.5, wide).height).toBeCloseTo(1.6, 10);
  });
});
