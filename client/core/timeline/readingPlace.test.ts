import { describe, expect, test } from "bun:test";
import { readingPlace, readingPlaceIndex } from "./readingPlace";

describe("a chat's reading place", () => {
  test("is the first row on screen, by its key, not a pixel offset", () => {
    const tokens = [
      { key: "m12", index: 12, isViewable: true },
      { key: "m10", index: 10, isViewable: true },
      { key: "m9", index: 9, isViewable: false },
      { key: "m11", index: 11, isViewable: true },
    ];
    expect(readingPlace(tokens)).toBe("m10");
    expect(readingPlace([])).toBeUndefined();
  });

  test("reopens at that row's index; a row that is gone keeps nothing", () => {
    const rows = [{ id: "d1" }, { id: "m9" }, { id: "m10" }, { id: "m11" }];
    expect(readingPlaceIndex(rows, "m10")).toBe(2);
    expect(readingPlaceIndex(rows, "gone")).toBeUndefined();
    expect(readingPlaceIndex(rows, undefined)).toBeUndefined();
  });
});
