import { describe, expect, test } from "bun:test";
import { statusTileLook } from "./colour";

describe("a Git status tile's colour (SPEC §10.4)", () => {
  test("A on add at 13 %, D on del at 11 %, M in the meter blue at 12 %, R in ink on the neutral tile", () => {
    expect(statusTileLook("added")).toEqual({ glyph: "add", wash: { of: "add", alpha: 0.13 } });
    expect(statusTileLook("deleted")).toEqual({ glyph: "del", wash: { of: "del", alpha: 0.11 } });
    expect(statusTileLook("modified")).toEqual({ glyph: "meter", wash: { of: "meter", alpha: 0.12 } });
    expect(statusTileLook("renamed")).toEqual({ glyph: "ink", wash: "tile" });
  });

  test("a conflict reads as deleted, an untracked file as added, a binary one stays faint", () => {
    expect(statusTileLook("conflict").glyph).toBe("del");
    expect(statusTileLook("untracked").glyph).toBe("add");
    expect(statusTileLook("binary")).toEqual({ glyph: "faint", wash: "canvas" });
  });
});
