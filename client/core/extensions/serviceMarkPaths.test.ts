import { describe, expect, test } from "bun:test";
import { hasServiceMark } from "./serviceMarkPaths";

describe("service marks", () => {
  test("Onlora draws its own brand mark, not a generic glyph", () => {
    expect(hasServiceMark("onlora")).toBe(true);
  });

  test("GitHub and Slack keep their brand logos", () => {
    expect(hasServiceMark("github")).toBe(true);
    expect(hasServiceMark("slack")).toBe(true);
  });

  test("Hyperliquid draws its own mark, not the generic chart glyph", () => {
    expect(hasServiceMark("hyperliquid")).toBe(true);
  });
});
