import { describe, expect, test } from "bun:test";
import { MAX_ANIMATED_NEW_ROWS, newRowKeys } from "./newRows";

describe("rows that fade in", () => {
  test("only the ones new since the list last changed", () => {
    expect([...newRowKeys(new Set(["a"]), ["a", "b"])]).toEqual(["b"]);
    expect(newRowKeys(new Set(["a", "b"]), ["a"]).size).toBe(0);
  });

  test("none on the first list, after an empty one, or for another computer's list", () => {
    expect(newRowKeys(null, ["a"]).size).toBe(0);
    expect(newRowKeys(new Set(), ["a"]).size).toBe(0);
    expect(newRowKeys(new Set(["a"]), ["x", "y"]).size).toBe(0);
  });

  test("none when more than 40 arrive at once", () => {
    const many = Array.from({ length: MAX_ANIMATED_NEW_ROWS + 1 }, (_, i) => `n${i}`);
    expect(newRowKeys(new Set(["a"]), ["a", ...many]).size).toBe(0);
    expect(newRowKeys(new Set(["a"]), ["a", ...many.slice(0, MAX_ANIMATED_NEW_ROWS)]).size).toBe(MAX_ANIMATED_NEW_ROWS);
  });
});
