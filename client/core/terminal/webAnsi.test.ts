import { describe, expect, test } from "bun:test";
import { rowText } from "./text";
import { ansiSpans, color256 } from "./webAnsi";

const text = (ansi: string) => ansiSpans(ansi).map((s) => s.t).join("");

describe("ansiSpans", () => {
  test("plain text is one unstyled run", () => {
    expect(ansiSpans("$ ls -la")).toEqual([{ t: "$ ls -la" }]);
    expect(ansiSpans("")).toEqual([]);
  });

  test("palette colors are the theme's variables; bold draws 0-7 bright; reset and 39/49 end them", () => {
    expect(ansiSpans("\u001b[31mred\u001b[0m \u001b[1;34mblue\u001b[22;39m x \u001b[42mbg\u001b[49m")).toEqual([
      { t: "red", c: "var(--c1)" },
      { t: " " },
      { t: "blue", c: "var(--c12)", f: "B" },
      { t: " x " },
      { t: "bg", g: "var(--c2)" },
    ]);
    expect(ansiSpans("\u001b[95mhi")).toEqual([{ t: "hi", c: "var(--c13)" }]);
    expect(ansiSpans("\u001b[107mhi")).toEqual([{ t: "hi", g: "var(--c15)" }]);
  });

  test("256 colors and truecolor, in semicolon and colon forms", () => {
    expect(ansiSpans("\u001b[38;5;196mx")).toEqual([{ t: "x", c: "#ff0000" }]);
    expect(ansiSpans("\u001b[38;5;3mx")).toEqual([{ t: "x", c: "var(--c3)" }]);
    expect(ansiSpans("\u001b[48;5;244mx")).toEqual([{ t: "x", g: "#808080" }]);
    expect(ansiSpans("\u001b[38;2;1;2;3mx")).toEqual([{ t: "x", c: "#010203" }]);
    expect(ansiSpans("\u001b[38:2::10:20:30mx")).toEqual([{ t: "x", c: "#0a141e" }]);
    expect(ansiSpans("\u001b[38:2:10:20:30mx")).toEqual([{ t: "x", c: "#0a141e" }]);
    expect(ansiSpans("\u001b[48:5:16mx")).toEqual([{ t: "x", g: "#000000" }]);
    // A color's parameters are not read as attributes of their own (1 of 38;2;1;... is not bold).
    expect(ansiSpans("\u001b[38;2;1;4;9mx")).toEqual([{ t: "x", c: "#010409" }]);
  });

  test("attributes are classes; inverse swaps (the theme's when unset); invisible hides", () => {
    expect(ansiSpans("\u001b[2;3;4;9;53mx")).toEqual([{ t: "x", f: "D I U S O" }]);
    expect(ansiSpans("\u001b[4:0mx")).toEqual([{ t: "x" }]);
    expect(ansiSpans("\u001b[4:3mx\u001b[24my")).toEqual([{ t: "x", f: "U" }, { t: "y" }]);
    expect(ansiSpans("\u001b[7mx")).toEqual([{ t: "x", c: "var(--bg)", g: "var(--fg)" }]);
    expect(ansiSpans("\u001b[7;31;42mx\u001b[27my")).toEqual([{ t: "x", c: "var(--c2)", g: "var(--c1)" }, { t: "y", c: "var(--c1)", g: "var(--c2)" }]);
    expect(ansiSpans("\u001b[8msecret\u001b[28mshown")).toEqual([{ t: "secret", c: "transparent" }, { t: "shown" }]);
  });

  test("runs of one style merge; other sequences and controls are dropped, the text is rowText's", () => {
    const row = "\u001b]8;;https://x.dev\u001b\\link\u001b]8;;\u001b\\ \u001b[?25l\u001b[1mbold\u001b[m\u001b[1m!\u001b[0m\u0007 \u001b(Bend\ttab";
    expect(text(row)).toBe(rowText(row));
    expect(ansiSpans("\u001b[1ma\u001b[1mb\u001b[0m\u001b[mc")).toEqual([{ t: "ab", f: "B" }, { t: "c" }]);
    expect(ansiSpans("\u001b[>4;2mx")).toEqual([{ t: "x" }]);
  });

  test("wide and astral characters pass through as they are", () => {
    expect(text("\u001b[32m中文 🐱\u001b[0m")).toBe("中文 🐱");
  });
});

describe("color256", () => {
  test("the cube and the grays, as xterm draws them", () => {
    expect(color256(16)).toBe("#000000");
    expect(color256(231)).toBe("#ffffff");
    expect(color256(67)).toBe("#5f87af");
    expect(color256(232)).toBe("#080808");
    expect(color256(255)).toBe("#eeeeee");
  });
});
