import { describe, expect, test } from "bun:test";
import {
  accessoryBottomOffset,
  androidKeyboardInset,
  keyboardCoversWindow,
  keyboardOverlap,
  nextAndroidBaseWindow,
} from "./terminalKeyboardInset";

const base = {
  platform: "ios",
  keyboardVisible: true,
  safeAreaBottom: 34,
  androidInset: 0,
};

describe("keyboardOverlap", () => {
  test("is the part of the window the keyboard covers", () => {
    expect(keyboardOverlap(951, { screenY: 660, height: 291 })).toBe(291);
    expect(keyboardOverlap(951, { screenY: 700, height: 291 })).toBe(251);
  });

  test("is nothing for a frame at or below the window's bottom", () => {
    expect(keyboardOverlap(951, { screenY: 951, height: 257 })).toBe(0);
    expect(keyboardOverlap(951, { screenY: 1200, height: 257 })).toBe(0);
    expect(keyboardOverlap(951, null)).toBe(0);
    expect(keyboardOverlap(951, { screenY: 600, height: 0 })).toBe(0);
  });

  test("never exceeds the keyboard's own height", () => {
    expect(keyboardOverlap(951, { screenY: 0, height: 291 })).toBe(291);
  });
});

describe("accessoryBottomOffset", () => {
  test("sits on the bottom inset while the keyboard is down", () => {
    expect(
      accessoryBottomOffset({
        ...base,
        keyboardVisible: false,
        windowHeight: 951,
        keyboardFrame: { screenY: 660, height: 291 },
      }),
    ).toBe(34);
  });

  test("sits on the keyboard while it covers the window", () => {
    expect(
      accessoryBottomOffset({
        ...base,
        windowHeight: 951,
        keyboardFrame: { screenY: 660, height: 291 },
      }),
    ).toBe(291);
  });

  // The Duo inner screen: landscape (669 high) then portrait (951 high) with a
  // keyboard "shown" whose frame sits below the window. Its height used to lift
  // the bar by 257pt and leave the bottom of the page empty.
  test("ignores a shown keyboard that covers nothing after a rotation", () => {
    const frame = { screenY: 951, height: 257 };
    expect(
      accessoryBottomOffset({ ...base, windowHeight: 951, keyboardFrame: frame }),
    ).toBe(34);
  });

  test("follows the keyboard's new frame when the window rotates under it", () => {
    const landscape = { screenY: 469, height: 200 };
    const portrait = { screenY: 660, height: 291 };
    expect(
      accessoryBottomOffset({ ...base, windowHeight: 669, keyboardFrame: landscape }),
    ).toBe(200);
    expect(
      accessoryBottomOffset({ ...base, windowHeight: 951, keyboardFrame: portrait }),
    ).toBe(291);
  });

  test("returns to the bottom inset once the keyboard hides", () => {
    expect(
      accessoryBottomOffset({
        ...base,
        keyboardVisible: false,
        windowHeight: 951,
        keyboardFrame: null,
      }),
    ).toBe(34);
  });

  test("on Android uses the inset the window resize left over", () => {
    expect(
      accessoryBottomOffset({
        ...base,
        platform: "android",
        windowHeight: 700,
        keyboardFrame: { screenY: 0, height: 300 },
        androidInset: 40,
      }),
    ).toBe(40);
    expect(
      accessoryBottomOffset({
        ...base,
        platform: "android",
        keyboardVisible: false,
        windowHeight: 1000,
        keyboardFrame: null,
        androidInset: 0,
      }),
    ).toBe(34);
  });
});

describe("androidKeyboardInset", () => {
  test("subtracts what the window already shrank by", () => {
    expect(androidKeyboardInset(300, 1000, 700)).toBe(0);
    expect(androidKeyboardInset(300, 1000, 760)).toBe(60);
    expect(androidKeyboardInset(300, 1000, 1000)).toBe(300);
  });

  test("takes the resize as handled when the base is unknown", () => {
    expect(androidKeyboardInset(300, null, 500)).toBe(0);
  });
});

describe("nextAndroidBaseWindow", () => {
  const portrait = { width: 400, height: 900 };
  const landscape = { width: 900, height: 400 };

  test("follows the window while the keyboard is down", () => {
    expect(nextAndroidBaseWindow(portrait, landscape, false)).toEqual(landscape);
  });

  test("keeps the base while the keyboard is up at the same width", () => {
    expect(nextAndroidBaseWindow(portrait, { width: 400, height: 600 }, true)).toBe(
      portrait,
    );
  });

  test("drops a base measured at another width", () => {
    expect(nextAndroidBaseWindow(portrait, landscape, true)).toBeNull();
    expect(nextAndroidBaseWindow(null, landscape, true)).toBeNull();
  });

  test("a rotation with the keyboard up no longer double-insets", () => {
    const base = nextAndroidBaseWindow(portrait, { width: 900, height: 250 }, true);
    expect(androidKeyboardInset(150, base?.height ?? null, 250)).toBe(0);
  });
});

describe("keyboardCoversWindow", () => {
  const covering = { imeVisible: true, imeHeight: 291, evidence: "keyboard_layout_guide" };
  const rotatedAway = { imeVisible: false, imeHeight: 0, evidence: "keyboard_layout_guide" };

  test("is down without a show event", () => {
    expect(keyboardCoversWindow(false, null)).toBe(false);
    expect(keyboardCoversWindow(false, covering)).toBe(false);
  });

  test("is up when shown and the window is covered", () => {
    expect(keyboardCoversWindow(true, covering)).toBe(true);
    expect(
      keyboardCoversWindow(true, { imeVisible: true, imeHeight: 200, evidence: "window_insets_ime" }),
    ).toBe(true);
  });

  test("is down when a missed hide left it 'shown' but nothing covers the window", () => {
    expect(keyboardCoversWindow(true, rotatedAway)).toBe(false);
    expect(
      keyboardCoversWindow(true, { imeVisible: true, imeHeight: 0, evidence: "keyboard_layout_guide" }),
    ).toBe(false);
  });

  test("trusts the event until the platform has answered", () => {
    expect(keyboardCoversWindow(true, null)).toBe(true);
  });

  test("a query that could not look decides nothing", () => {
    for (const evidence of [
      "native_module_unavailable",
      "window_unavailable",
      "activity_unavailable",
      "insets_unavailable",
      "snapshot_failed",
    ]) {
      expect(
        keyboardCoversWindow(true, { imeVisible: false, imeHeight: 0, evidence }),
      ).toBe(true);
    }
  });

  test("the key bar drops to the safe area once nothing covers the window", () => {
    const shown = true;
    const visible = keyboardCoversWindow(shown, rotatedAway);
    expect(
      accessoryBottomOffset({
        platform: "ios",
        keyboardVisible: visible,
        safeAreaBottom: 34,
        windowHeight: 951,
        keyboardFrame: { screenY: 660, height: 291 },
        androidInset: 0,
      }),
    ).toBe(34);
  });
});
