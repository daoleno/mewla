import { describe, expect, test } from "bun:test";
import { findMatches, sectionLines, stepHunk, stepIndex, type NavSection } from "./runtime/diffNavigation";

// @@ -10,4 +10,5 @@ : one context line, a change that swaps one line for two, one context line.
const section: NavSection = {
  id: "s",
  additionLines: ["keep top", "new one", "new two", "keep end"],
  deletionLines: ["keep top", "old one", "keep end"],
  hunks: [
    {
      additionStart: 10,
      deletionStart: 10,
      hunkContent: [
        { type: "context", lines: 1, additionLineIndex: 0, deletionLineIndex: 0 },
        { type: "change", deletions: 1, deletionLineIndex: 1, additions: 2, additionLineIndex: 1 },
        { type: "context", lines: 1, additionLineIndex: 3, deletionLineIndex: 2 },
      ],
    },
  ],
};

describe("diff reader line model", () => {
  test("context reads once on the new side and deletions precede additions", () => {
    expect(sectionLines(section).map((l) => `${l.side}:${l.line}:${l.text}`)).toEqual([
      "additions:10:keep top",
      "deletions:11:old one",
      "additions:11:new one",
      "additions:12:new two",
      "additions:13:keep end",
    ]);
  });

  test("find is case-insensitive and reports the side and line of each hit", () => {
    expect(findMatches([section], "NEW")).toEqual([
      { id: "s", side: "additions", line: 11 },
      { id: "s", side: "additions", line: 12 },
    ]);
    expect(findMatches([section], "old")).toEqual([{ id: "s", side: "deletions", line: 11 }]);
    expect(findMatches([section], "")).toEqual([]);
    expect(findMatches([section], "absent")).toEqual([]);
  });
});

describe("stepping", () => {
  test("matches wrap around in both directions", () => {
    expect(stepIndex(-1, 3, "next")).toBe(0);
    expect(stepIndex(-1, 3, "previous")).toBe(2);
    expect(stepIndex(2, 3, "next")).toBe(0);
    expect(stepIndex(0, 3, "previous")).toBe(2);
    expect(stepIndex(0, 0, "next")).toBe(-1);
  });

  test("hunks clamp at the ends instead of jumping across the file", () => {
    expect(stepHunk(-1, 4, "next")).toBe(0);
    expect(stepHunk(-1, 4, "previous")).toBe(3);
    expect(stepHunk(3, 4, "next")).toBe(3);
    expect(stepHunk(0, 4, "previous")).toBe(0);
    expect(stepHunk(1, 4, "next")).toBe(2);
    expect(stepHunk(0, 0, "next")).toBe(-1);
  });
});
