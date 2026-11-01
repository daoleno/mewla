import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  nextIosKeyboardFrame,
  snapshotWithKeyboardFrame,
} from "./iosKeyboardSnapshot";
import {
  createStructuredChatKeyboardLifecycleGate,
  reduceStructuredChatKeyboardLifecycleGate,
  structuredChatGatedOverlayTranslateY,
} from "./chatKeyboardOverlayPolicy";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

// iPhone 18 Pro portrait from the Mac log: window 402x874, keyboard 329pt.
const WINDOW = 874;
const SHOWN = { screenY: 545, height: 329 };
// What the native layout guide answered with that keyboard up.
const NATIVE_SAYS_NOTHING = {
  revision: 1,
  imeVisible: false,
  imeHeight: 0,
  composerFocused: true,
  evidence: "keyboard_layout_guide",
};

describe("iOS interface keyboard fact", () => {
  test("a real keyboard counts although the native layout guide reads zero", () => {
    const snapshot = snapshotWithKeyboardFrame(
      NATIVE_SAYS_NOTHING,
      WINDOW,
      SHOWN,
    );
    expect(snapshot.imeVisible).toBe(true);
    expect(snapshot.imeHeight).toBe(329);
    // Which view is focused stays the native answer.
    expect(snapshot.composerFocused).toBe(true);
  });

  test("the Composer gate opens and lifts by the covered height", () => {
    const snapshot = snapshotWithKeyboardFrame(
      NATIVE_SAYS_NOTHING,
      WINDOW,
      SHOWN,
    );
    let gate = createStructuredChatKeyboardLifecycleGate({
      enabled: true,
      appActive: true,
    });
    gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
      type: "composer_focus",
      focused: true,
    });
    // Before the fix: the raw snapshot kept the gate closed (translateY 0).
    const before = reduceStructuredChatKeyboardLifecycleGate(gate, {
      type: "authoritative_snapshot",
      sourceRevision: gate.revision,
      imeVisible: NATIVE_SAYS_NOTHING.imeVisible,
      imeHeight: NATIVE_SAYS_NOTHING.imeHeight,
      composerFocused: NATIVE_SAYS_NOTHING.composerFocused,
      foregroundReconciliation: false,
    });
    expect(
      structuredChatGatedOverlayTranslateY({
        gate: before,
        keyboardVerticalOffset: 0,
      }),
    ).toBe(0);

    const after = reduceStructuredChatKeyboardLifecycleGate(gate, {
      type: "authoritative_snapshot",
      sourceRevision: gate.revision,
      imeVisible: snapshot.imeVisible,
      imeHeight: snapshot.imeHeight,
      composerFocused: snapshot.composerFocused,
      foregroundReconciliation: false,
    });
    expect(
      structuredChatGatedOverlayTranslateY({
        gate: after,
        keyboardVerticalOffset: 0,
      }),
    ).toBe(-329);
  });

  test("a frame that leaves the window covers nothing (rotation, fold)", () => {
    const afterRotation = { screenY: 874, height: 257 };
    const snapshot = snapshotWithKeyboardFrame(
      { ...NATIVE_SAYS_NOTHING, imeVisible: true, imeHeight: 257 },
      WINDOW,
      afterRotation,
    );
    expect(snapshot.imeVisible).toBe(false);
    expect(snapshot.imeHeight).toBe(0);
  });

  test("the height is the overlap, not the event's raw height", () => {
    const snapshot = snapshotWithKeyboardFrame(
      NATIVE_SAYS_NOTHING,
      WINDOW,
      { screenY: 700, height: 329 },
    );
    expect(snapshot.imeHeight).toBe(174);
  });

  test("no frame means no keyboard", () => {
    expect(
      snapshotWithKeyboardFrame(NATIVE_SAYS_NOTHING, WINDOW, null).imeVisible,
    ).toBe(false);
  });

  test("showing and moving frames are read at once", () => {
    for (const name of [
      "keyboardWillShow",
      "keyboardDidShow",
      "keyboardWillChangeFrame",
      "keyboardDidChangeFrame",
    ] as const) {
      expect(nextIosKeyboardFrame(name, SHOWN, WINDOW)).toEqual({
        frame: SHOWN,
        reread: true,
      });
    }
  });

  test("a hide is recorded without pulling the Composer down early", () => {
    const leaving = { screenY: 874, height: 329 };
    expect(nextIosKeyboardFrame("keyboardWillHide", null, WINDOW)).toEqual({
      frame: null,
      reread: false,
    });
    expect(
      nextIosKeyboardFrame("keyboardWillChangeFrame", leaving, WINDOW),
    ).toEqual({ frame: null, reread: false });
    // Its end re-reads, so a lost end sample cannot strand the Composer.
    expect(
      nextIosKeyboardFrame("keyboardDidChangeFrame", leaving, WINDOW),
    ).toEqual({ frame: null, reread: true });
    expect(nextIosKeyboardFrame("keyboardDidHide", null, WINDOW)).toEqual({
      frame: null,
      reread: true,
    });
  });

  test("only iOS reconciles, and the overlap is the terminal's keyboardOverlap", () => {
    const frame = source("InterfaceChatKeyboardFrame.tsx");
    expect(frame).toContain('if (Platform.OS !== "ios")');
    expect(frame).toContain("snapshotWithKeyboardFrame(");
    expect(source("iosKeyboardSnapshot.ts")).toContain(
      'from "./terminalKeyboardInset"',
    );
    // Android's snapshot path stays the native IME answer.
    expect(frame).toContain("return native;");
  });
});
