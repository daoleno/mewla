import { describe, expect, test } from "bun:test";
import { FOLDED_TEXT_HEIGHT, MESSAGE_FOLD, messageFolds } from "./messageFold";

describe("a long sent message folds", () => {
  test("past 8 lines of its text, not at 8", () => {
    expect(messageFolds(8 * MESSAGE_FOLD.line)).toBe(false);
    expect(messageFolds(8 * MESSAGE_FOLD.line + 4)).toBe(false);
    expect(messageFolds(9 * MESSAGE_FOLD.line)).toBe(true);
    expect(messageFolds(40 * MESSAGE_FOLD.line)).toBe(true);
  });

  test("folded, it shows its first 6 lines", () => {
    expect(FOLDED_TEXT_HEIGHT).toBe(6 * 25);
  });
});
