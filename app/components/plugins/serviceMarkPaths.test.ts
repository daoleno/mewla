import { describe, expect, test } from "bun:test";
import { hasServiceMark } from "./serviceMarkPaths";

describe("service marks", () => {
  test("Onlora draws its own brand mark, not a generic glyph", () => {
    expect(hasServiceMark("onlora")).toBe(true);
  });
});
