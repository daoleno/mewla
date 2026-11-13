import { describe, expect, test } from "bun:test";
import { GROUP_PADDING, terminalGroupPadding, terminalRows as rows, terminalTight, TIGHT_PADDING } from "./keyboardRoom";

describe("the terminal over the keyboard on a short window", () => {
  test("a phone on its side keeps a prompt and its answer in view (SE 49 pt, 18 Pro 58 pt groups in the audit)", () => {
    for (const room of [49, 58]) {
      expect(rows(room, false)).toBeLessThan(2);
      expect(terminalTight({ keyboard: 206, room })).toBe(true);
      expect(rows(room, true)).toBeGreaterThanOrEqual(3);
      expect(rows(room, true)).toBeGreaterThan(rows(room, false) + 1);
    }
  });

  test("a tall window and a hidden keyboard keep the usual padding", () => {
    expect(terminalTight({ keyboard: 264, room: 285 })).toBe(false); // Duo inner on its side
    expect(terminalTight({ keyboard: 336, room: 398 })).toBe(false); // Duo inner upright
    expect(terminalTight({ keyboard: 0, room: 49 })).toBe(false);
    expect(terminalGroupPadding(false)).toEqual(GROUP_PADDING);
  });

  test("tight only gives up vertical padding", () => {
    expect(TIGHT_PADDING.left).toBe(GROUP_PADDING.left);
    expect(TIGHT_PADDING.right).toBe(GROUP_PADDING.right);
    expect(TIGHT_PADDING.top + TIGHT_PADDING.bottom).toBeLessThan(GROUP_PADDING.top + GROUP_PADDING.bottom);
  });
});
