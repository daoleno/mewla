import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join } from "node:path";

/**
 * The drawer's visuals (offset, backdrop opacity and touchability) are
 * Reanimated styles. Reanimated 4.3–4.5.2 hands settled values to React from a
 * 500 ms JS timer but evicted them by age first, so a value the timer missed —
 * timers paused in the background, or a JS thread busy on resume — never
 * reached React. The next React commit then restored the stale value: a
 * closed drawer reappeared open, and taps or swipes changed nothing because
 * the UI-thread target was already closed (software-mansion/
 * react-native-reanimated#9574, #10805; fixed by #9527 in 4.5.3).
 *
 * `expo install --fix` would pin SDK 57's 4.5.1 again, so keep the floor here.
 */
const FIRST_FIXED_VERSION = [4, 5, 3] as const;

const appRoot = join(import.meta.dir, "..", "..");
const requireFromApp = createRequire(join(appRoot, "package.json"));

function readJson(path: string): Record<string, any> {
  return JSON.parse(readFileSync(path, "utf8"));
}

function parseVersion(version: string): number[] {
  const match = /^(\d+)\.(\d+)\.(\d+)$/.exec(version);
  if (match == null) {
    throw new Error(`Expected an exact x.y.z version, got ${version}`);
  }
  return match.slice(1).map(Number);
}

function atLeast(version: number[], floor: readonly number[]): boolean {
  for (let i = 0; i < floor.length; i++) {
    if (version[i] !== floor[i]) {
      return version[i] > floor[i];
    }
  }
  return true;
}

describe("Reanimated settled animated props", () => {
  const appPackage = readJson(join(appRoot, "package.json"));
  const settledPropsFlag =
    appPackage.reanimated?.staticFeatureFlags
      ?.FORCE_REACT_RENDER_FOR_SETTLED_ANIMATIONS;

  test("the app pins a Reanimated that never drops an unsynced settled value", () => {
    const declared = appPackage.dependencies["react-native-reanimated"];
    expect(
      settledPropsFlag === false ||
        atLeast(parseVersion(declared), FIRST_FIXED_VERSION),
    ).toBe(true);
  });

  test("the installed Reanimated is the fixed one", () => {
    const installed = readJson(
      requireFromApp.resolve("react-native-reanimated/package.json"),
    ).version;
    expect(
      settledPropsFlag === false ||
        atLeast(parseVersion(installed), FIRST_FIXED_VERSION),
    ).toBe(true);
  });

  test("Expo's SDK pin cannot silently downgrade it", () => {
    expect(appPackage.expo?.install?.exclude).toContain(
      "react-native-reanimated",
    );
  });
});
