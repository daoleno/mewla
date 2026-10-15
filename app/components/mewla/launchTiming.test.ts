import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { LAUNCH, SPLASH_IMAGE_SIDE, launchExitAt, splashSealBox } from "./launchTiming";

describe("launch veil timing", () => {
  test("waits for the app, never for the animation past 1.2 s", () => {
    expect(launchExitAt({ readyAt: null, reducedMotion: false })).toBeNull();
    // Ready before the cat is out of the seal: leave as it hops out.
    expect(launchExitAt({ readyAt: -300, reducedMotion: false })).toBe(LAUNCH.outOfSealMs);
    expect(launchExitAt({ readyAt: 200, reducedMotion: false })! + LAUNCH.exitMs).toBeLessThanOrEqual(1200);
    // A slow start leaves the moment it is ready.
    expect(launchExitAt({ readyAt: 2600, reducedMotion: false })).toBe(2600);
  });
  test("without motion the splash only fades once the app is ready", () => {
    expect(launchExitAt({ readyAt: -50, reducedMotion: true })).toBe(0);
    expect(launchExitAt({ readyAt: 900, reducedMotion: true })).toBe(900);
    expect(LAUNCH.reducedExitMs).toBeLessThanOrEqual(150);
  });
});

describe("splash hand-off", () => {
  test("matches the native splash's size", () => {
    const config = JSON.parse(readFileSync(join(import.meta.dir, "../../app.base.json"), "utf8"));
    const plugin = config.expo.plugins.find((entry: unknown) => Array.isArray(entry) && entry[0] === "expo-splash-screen");
    expect(plugin[1].imageWidth).toBe(SPLASH_IMAGE_SIDE);
  });
  test("puts the clip's seal on the splash seal", () => {
    const box = splashSealBox(390, 844);
    // A clip's seal is 227/256 of its box, centred 11/256 above the centre.
    expect((box.size * 227) / 256).toBeCloseTo((SPLASH_IMAGE_SIDE * 352) / 512, 5);
    expect(box.cy - (11 / 256) * box.size).toBeCloseTo(422, 5);
    expect(box.cx).toBe(195);
  });
});
