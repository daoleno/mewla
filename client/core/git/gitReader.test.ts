import { describe, expect, test } from "bun:test";
import { parseGitPatch } from "./gitDiffHunks";
import { buildGitReviewGroups, gitReviewOrder } from "./gitReviewModel";
import { findGitPatchMatches, followGitReviewEntry, gitAskContext, gitFindLabel, stepGitCodeText, stepGitFind } from "./gitReader";

const PATCH = [
  "diff --git a/app.ts b/app.ts",
  "--- a/app.ts",
  "+++ b/app.ts",
  "@@ -10,3 +10,4 @@ function run()",
  " keep top",
  "-old one",
  "+new one",
  "+new two",
  " keep end",
  "@@ -40,2 +41,2 @@",
  "-Old tail",
  "+new tail",
  " end",
].join("\n");

describe("find in the diff", () => {
  test("finds lines case-insensitively in reading order, with their side, line and hunk", () => {
    const parsed = parseGitPatch(PATCH);
    expect(findGitPatchMatches(parsed, "NEW")).toEqual([
      { side: "additions", line: 11, hunk: 0 },
      { side: "additions", line: 12, hunk: 0 },
      { side: "additions", line: 41, hunk: 1 },
    ]);
    expect(findGitPatchMatches(parsed, "old")).toEqual([
      { side: "deletions", line: 11, hunk: 0 },
      { side: "deletions", line: 40, hunk: 1 },
    ]);
    expect(findGitPatchMatches(parsed, "keep")).toEqual([
      { side: "additions", line: 10, hunk: 0 },
      { side: "additions", line: 13, hunk: 0 },
    ]);
    expect(findGitPatchMatches(parsed, "  ")).toEqual([]);
    expect(findGitPatchMatches(null, "x")).toEqual([]);
  });

  test("steps around the ends and labels the count as the old bar did", () => {
    expect(stepGitFind(-1, 3, "next")).toBe(0);
    expect(stepGitFind(-1, 3, "previous")).toBe(2);
    expect(stepGitFind(2, 3, "next")).toBe(0);
    expect(stepGitFind(0, 3, "previous")).toBe(2);
    expect(stepGitFind(0, 0, "next")).toBe(-1);
    expect(gitFindLabel("", 0, -1)).toBe("");
    expect(gitFindLabel("x", 0, -1)).toBe("No matches");
    expect(gitFindLabel("x", 4, -1)).toBe("4");
    expect(gitFindLabel("x", 4, 1)).toBe("2/4");
  });
});

describe("the reader after its file leaves its group", () => {
  const file = (path: string, staged: boolean, unstaged: boolean) => ({ path, status: "modified", staged, unstaged, untracked: false });

  test("moves to the entry now at the same place in the whole order, and closes only when nothing is left", () => {
    // Before: staged a.ts | changes b.ts. Staging b.ts leaves staged a.ts, b.ts.
    const after = gitReviewOrder(buildGitReviewGroups([file("a.ts", true, false), file("b.ts", true, false)]));
    expect(followGitReviewEntry(after, 1)?.key).toBe("staged:b.ts");
    // The last of all left: the one before it.
    expect(followGitReviewEntry(after, 5)?.key).toBe("staged:b.ts");
    expect(followGitReviewEntry([], 0)).toBeNull();
  });
});

describe("display options", () => {
  test("code text steps by 2 between 10 and 20", () => {
    expect(stepGitCodeText(12, "larger")).toBe(14);
    expect(stepGitCodeText(20, "larger")).toBe(20);
    expect(stepGitCodeText(10, "smaller")).toBe(10);
  });
});

describe("Ask the agent", () => {
  const hunk = parseGitPatch(PATCH).hunks[0]!;

  test("selected lines keep their range, are quoted, and name the working-tree diff", () => {
    const ask = gitAskContext({ path: "app.ts", group: "changes", hunk, selection: { side: "additions", start: 11, end: 12 } });
    expect(ask.where).toBe("app.ts:11-12");
    expect(ask.quote).toEqual(["+ new one", "+ new two"]);
    expect(ask.build("Use a constant")).toBe("In the working-tree diff, app.ts:11-12:\n> + new one\n> + new two\n\nUse a constant");
  });

  test("removed lines say so; a staged file says staged; no selection is the whole file", () => {
    const removed = gitAskContext({ path: "app.ts", group: "staged", hunk, selection: { side: "deletions", start: 11, end: 11 } });
    expect(removed.where).toBe("app.ts:11");
    expect(removed.build("Why?")).toBe("In the staged diff, app.ts:11 (removed line, as it was before the change):\n> - old one\n\nWhy?");
    const whole = gitAskContext({ path: "app.ts", group: "changes", hunk: null, selection: null });
    expect(whole.where).toBe("app.ts");
    expect(whole.quote).toEqual([]);
    expect(whole.build("  Tidy this  ")).toBe("In the working-tree diff, app.ts:\n\nTidy this");
  });
});
