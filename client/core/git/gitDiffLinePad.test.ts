import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { GIT_DIFF_LINE_PAD, padGitDiffPatch, padGitDiffText } from "./gitDiffLinePad";

// R4#3: on Android a line ending in an italic comment broke its last word onto the next row.
// nsh test measures text by a fixed width per character, so the overlap does not show headless:
// these hold what Git review now hands DiffView (the device shots are in FIXES-ROUND4.md).
const PAD = GIT_DIFF_LINE_PAD;

describe("Git review pads the lines DiffView draws (R4#3)", () => {
  test("the pad is spaces: advance with no ink", () => {
    expect(PAD.length).toBeGreaterThan(0);
    expect(PAD.trim()).toBe("");
  });

  test("a file's non-empty lines end in the pad; its line count and endings stay", () => {
    const text = "export const a = 1; // one\n\nb\r\nlast";
    const padded = padGitDiffText(text);
    expect(padded).toBe(`export const a = 1; // one${PAD}\n\nb${PAD}\r\nlast${PAD}`);
    expect(padded.split("\n").length).toBe(text.split("\n").length);
    // A final newline adds no line.
    expect(padGitDiffText("a\nb\n")).toBe(`a${PAD}\nb${PAD}\n`);
  });

  test("a patch pads its hunk lines only: headers, empty lines and the no-newline mark stay", () => {
    const patch = [
      "diff --git a/x.ts b/x.ts",
      "index 1111111..2222222 100644",
      "--- a/x.ts",
      "+++ b/x.ts",
      "@@ -1,3 +1,3 @@",
      " keep // comment",
      "-old",
      "+new",
      "+",
      "--- removed line that looks like a header",
      "\\ No newline at end of file",
      "diff --git a/y.ts b/y.ts",
      "--- a/y.ts",
      "+++ b/y.ts",
      "@@ -0,0 +1 @@",
      "+only\r",
      "",
    ].join("\n");
    expect(padGitDiffPatch(patch).split("\n")).toEqual([
      "diff --git a/x.ts b/x.ts",
      "index 1111111..2222222 100644",
      "--- a/x.ts",
      "+++ b/x.ts",
      "@@ -1,3 +1,3 @@",
      ` keep // comment${PAD}`,
      `-old${PAD}`,
      `+new${PAD}`,
      "+",
      `--- removed line that looks like a header${PAD}`,
      "\\ No newline at end of file",
      "diff --git a/y.ts b/y.ts",
      "--- a/y.ts",
      "+++ b/y.ts",
      "@@ -0,0 +1 @@",
      `+only${PAD}\r`,
      "",
    ]);
  });

  test("the reader and the file viewer hand DiffView the padded text", () => {
    const dir = join(import.meta.dir, "../../ui/more/git");
    const reader = readFileSync(join(dir, "GitDiff.tsx"), "utf8");
    expect(reader).toContain('patch={props.sides() ? null : padGitDiffPatch(props.section()?.patch ?? "")}');
    expect(reader).toContain("oldValue={nullablePad(props.sides()?.oldContents)}");
    expect(reader).toContain("newValue={nullablePad(props.sides()?.newContents)}");
    const files = readFileSync(join(dir, "GitFiles.tsx"), "utf8");
    expect(files).toContain("const shown = padGitDiffText(content);");
    expect(files).toContain("oldValue={shown} newValue={shown}");
  });
});
