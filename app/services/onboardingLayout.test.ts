import { describe, expect, test } from "bun:test";
import {
  catBoxForStep,
  cornerCatBox,
  heroCatBox,
  heroZoneWidth,
  isCompactFrame,
  onboardingHeroBox,
} from "../components/onboarding/onboardingModel";

// The onboarding screen sits inside the side insets, so its frame is the
// window minus left and right.
const frame = (width: number, height: number, top: number, bottom: number) => ({ width, height, top, bottom });

describe("onboarding layout on short windows", () => {
  test("a phone upright keeps the cat above the text, where it always stood", () => {
    for (const f of [frame(402, 874, 62, 34), frame(375, 667, 20, 0), frame(360, 800, 24, 0), frame(820, 1180, 24, 20)]) {
      expect(isCompactFrame(f)).toBe(false);
      expect(heroZoneWidth(f)).toBe(0);
      const usable = f.height - f.top - f.bottom;
      expect(heroCatBox(f)).toEqual({ cx: f.width / 2, cy: f.top + Math.max(132, Math.round(usable * 0.27)), size: 164 });
      expect(catBoxForStep("welcome", f)).toEqual(heroCatBox(f));
    }
  });

  test("windows shorter than 520 usable points are compact", () => {
    // iPhone Duo outer screen on its side, 84 on the right or the left.
    expect(isCompactFrame(frame(594, 466, 0, 34))).toBe(true);
    // An ordinary phone on its side.
    expect(isCompactFrame(frame(750, 402, 0, 21))).toBe(true);
    expect(isCompactFrame(frame(800, 360, 24, 0))).toBe(true);
    expect(isCompactFrame(frame(600, 560, 0, 0))).toBe(false);
  });

  test("compact: the cat has a column of its own and fits the usable height", () => {
    for (const f of [frame(594, 466, 0, 34), frame(750, 402, 0, 21), frame(800, 360, 24, 0)]) {
      const usable = f.height - f.top - f.bottom;
      const zone = heroZoneWidth(f);
      const box = heroCatBox(f);
      expect(zone).toBeGreaterThan(0);
      expect(zone).toBeLessThanOrEqual(260);
      expect(zone).toBeLessThan(f.width / 2);
      // Inside its column, and inside the usable height.
      expect(box.cx - box.size / 2).toBeGreaterThanOrEqual(0);
      expect(box.cx + box.size / 2).toBeLessThanOrEqual(zone);
      expect(box.cy - box.size / 2).toBeGreaterThanOrEqual(f.top);
      expect(box.cy + box.size / 2).toBeLessThanOrEqual(f.top + usable);
    }
  });

  test("compact corner cat stays inside the frame, clear of the right inset", () => {
    for (const f of [frame(594, 466, 0, 34), frame(750, 402, 0, 21)]) {
      const corner = cornerCatBox(f);
      expect(corner.cx + corner.size / 2).toBeLessThanOrEqual(f.width);
      expect(catBoxForStep("scan", f)).toEqual(corner);
    }
  });

  test("the launch veil lands the cat where the screen inside the side insets puts it", () => {
    // A symmetric window: nothing moves.
    const symmetric = onboardingHeroBox({ width: 874, height: 402, top: 0, bottom: 21, left: 62, right: 62 });
    expect(symmetric.cx).toBe(62 + heroCatBox(frame(750, 402, 0, 21)).cx);
    // An uneven window (the Duo's 84-point rail) shifts with the left inset.
    const rail = onboardingHeroBox({ width: 678, height: 466, top: 0, bottom: 34, left: 0, right: 84 });
    expect(rail).toEqual(heroCatBox(frame(594, 466, 0, 34)));
    const portrait = onboardingHeroBox({ width: 402, height: 874, top: 62, bottom: 34, left: 0, right: 0 });
    expect(portrait).toEqual(heroCatBox(frame(402, 874, 62, 34)));
  });
});
