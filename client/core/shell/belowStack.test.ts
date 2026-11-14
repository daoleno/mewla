import { describe, expect, test } from "bun:test";
import { belowStack, keyboardLift } from "./belowStack";

describe("what lies below the stack follows the window and the pane as they are now", () => {
  test("a pane that fills the window leaves nothing below", () => {
    expect(belowStack(669, 669)).toBe(0);
    expect(belowStack(951, 951)).toBe(0);
  });

  test("Android's navigation bar under an edge-to-edge stack is below it", () => {
    expect(belowStack(800, 752)).toBe(48);
    expect(keyboardLift(300, 48)).toBe(252);
  });

  test("a rotation settles whichever of the two arrives first (Duo inner 669 × 951 → 951 × 669)", () => {
    // The pane laid out at its new height while the window still said the old one: a moment's 282 ...
    const during = belowStack(951, 669);
    expect(during).toBe(282);
    // ... and once the window says the new height, nothing is below again, so the keyboard lifts the foot fully.
    const after = belowStack(669, 669);
    expect(after).toBe(0);
    expect(keyboardLift(264, after)).toBe(264);
    // The other order: the window first, the pane's layout after it.
    expect(belowStack(669, 951)).toBe(0);
  });

  test("before the pane has laid out nothing is below", () => {
    expect(belowStack(844, null)).toBe(0);
  });
});
