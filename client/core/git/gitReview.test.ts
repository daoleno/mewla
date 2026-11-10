import { describe, expect, test } from "bun:test";
import type { GitDiffFileInfo, GitDiffStatusSnapshot } from "./gitDiff";
import { formatGitAskAgentMessage } from "./gitAskAgent";
import { gitHunkAtLine, gitHunkChangeCounts, gitHunkSelection, gitSelectedLines, parseGitPatch } from "./gitDiffHunks";
import {
  adjacentGitReviewEntry,
  buildGitReviewGroups,
  buildGitReviewRows,
  gitReviewOrder,
  gitReviewPaths,
  gitReviewSummary,
  gitReviewSync,
  matchesGitReviewQuery,
} from "./gitReviewModel";

const file = (path: string, extra: Partial<GitDiffFileInfo> = {}): GitDiffFileInfo => ({
  path,
  status: "modified",
  staged: false,
  unstaged: true,
  untracked: false,
  additions: 1,
  deletions: 1,
  working_additions: 1,
  working_deletions: 1,
  ...extra,
});

describe("git review groups", () => {
  const files = [
    file("a.ts", { staged: true, unstaged: true, staged_additions: 4, staged_deletions: 2 }),
    file("b.ts", { staged: true, unstaged: false, staged_additions: 3, staged_deletions: 0, working_additions: 0, working_deletions: 0 }),
    file("dir/c.ts", { status: "untracked", unstaged: false, untracked: true, working_additions: 9, working_deletions: 0 }),
  ];

  test("a file edited both ways is listed once per group, with that group's counts", () => {
    const groups = buildGitReviewGroups(files);
    expect(groups.staged.map((entry) => [entry.key, entry.additions, entry.deletions])).toEqual([
      ["staged:a.ts", 4, 2],
      ["staged:b.ts", 3, 0],
    ]);
    expect(groups.changes.map((entry) => [entry.key, entry.additions])).toEqual([
      ["changes:a.ts", 1],
      ["changes:dir/c.ts", 9],
    ]);
  });

  test("reading order is staged then changes, and neighbours stop at the ends", () => {
    const order = gitReviewOrder(buildGitReviewGroups(files));
    expect(order.map((entry) => entry.key)).toEqual(["staged:a.ts", "staged:b.ts", "changes:a.ts", "changes:dir/c.ts"]);
    expect(adjacentGitReviewEntry(order, "staged:b.ts", "next")?.key).toBe("changes:a.ts");
    expect(adjacentGitReviewEntry(order, "staged:a.ts", "previous")).toBeNull();
    expect(adjacentGitReviewEntry(order, "changes:dir/c.ts", "next")).toBeNull();
  });

  test("stage and unstage requests carry rename sources once", () => {
    const entries = buildGitReviewGroups([file("new.ts", { old_path: "old.ts", staged: true }), file("x.ts", { staged: true })]).staged;
    expect(gitReviewPaths(entries)).toEqual(["new.ts", "old.ts", "x.ts"]);
  });

  test("summary and sync read once", () => {
    expect(gitReviewSummary({ available: true, clean: false, file_count: 1, additions: 3, deletions: 0 } as never)).toBe("+3 −0 · 1 file");
    expect(gitReviewSummary({ available: true, clean: true } as never)).toBe("");
    expect(gitReviewSync({ ahead: 2, behind: 1 })).toBe("↑2 ↓1");
    expect(gitReviewSync({ ahead: 0, behind: 0 })).toBe("");
  });
});

describe("git review rows", () => {
  const entries = buildGitReviewGroups([
    file("app/components/screen/a.ts"),
    file("app/components/screen/b.ts"),
    file("app/components/screen/runtime/c.ts"),
    file("daemon/x.go"),
    file("README.md"),
  ]).changes;

  test("list mode keeps the entries in order", () => {
    const rows = buildGitReviewRows(entries, { tree: false, collapsed: new Set(), query: "", scope: "changes" });
    expect(rows.map((row) => row.key)).toEqual(entries.map((entry) => entry.key));
  });

  test("tree mode joins single-folder chains and nests the rest", () => {
    const rows = buildGitReviewRows(entries, { tree: true, collapsed: new Set(), query: "", scope: "changes" });
    expect(rows.map((row) => (row.kind === "dir" ? `${row.depth}:${row.label}/` : `${row.depth}:${row.entry.file.path}`))).toEqual([
      "0:app/components/screen/",
      "1:runtime/",
      "2:app/components/screen/runtime/c.ts",
      "1:app/components/screen/a.ts",
      "1:app/components/screen/b.ts",
      "0:daemon/",
      "1:daemon/x.go",
      "0:README.md",
    ]);
  });

  test("a collapsed folder hides its rows and counts them", () => {
    const rows = buildGitReviewRows(entries, {
      tree: true,
      collapsed: new Set(["changes:dir:app/components/screen"]),
      query: "",
      scope: "changes",
    });
    expect(rows[0]).toMatchObject({ kind: "dir", count: 3, collapsed: true });
    expect(rows.some((row) => row.kind === "file" && row.entry.file.path.startsWith("app/"))).toBe(false);
  });

  test("search narrows by every word, case-insensitively", () => {
    expect(matchesGitReviewQuery("app/Screen/a.ts", "screen a.ts")).toBe(true);
    expect(matchesGitReviewQuery("app/Screen/a.ts", "screen go")).toBe(false);
    const rows = buildGitReviewRows(entries, { tree: false, collapsed: new Set(), query: "DAEMON", scope: "changes" });
    expect(rows).toHaveLength(1);
  });
});

const PATCH = [
  "diff --git a/f.ts b/f.ts",
  "index 1..2 100644",
  "--- a/f.ts",
  "+++ b/f.ts",
  "@@ -1,4 +1,4 @@ head",
  " one",
  "-two",
  "+TWO",
  " three",
  " four",
  "@@ -20,3 +20,4 @@",
  " twenty",
  "+added",
  " twenty-one",
  "\\ No newline at end of file",
  " twenty-two",
].join("\n");

describe("git patch hunks", () => {
  const parsed = parseGitPatch(PATCH);

  test("splits at @@ headers and numbers both sides", () => {
    expect(parsed.hunks).toHaveLength(2);
    expect(parsed.hunks[0]).toMatchObject({ oldStart: 1, oldCount: 4, newStart: 1, newCount: 4 });
    expect(parsed.hunks[0].lines.map((line) => [line.kind, line.oldLine, line.newLine])).toEqual([
      [" ", 1, 1],
      ["-", 2, null],
      ["+", null, 2],
      [" ", 3, 3],
      [" ", 4, 4],
    ]);
    expect(parsed.hunks[1].lines.map((line) => line.newLine)).toEqual([20, 21, 22, 23]);
  });

  test("hunk text starts at the @@ line, ends with a newline and keeps the no-newline marker", () => {
    expect(parsed.hunks[0].text.startsWith("@@ -1,4 +1,4 @@ head\n one\n-two\n")).toBe(true);
    expect(parsed.hunks[1].text.endsWith(" twenty-two\n")).toBe(true);
    expect(parsed.hunks[1].text).toContain("\\ No newline at end of file\n");
  });

  test("a line maps to its hunk on either side", () => {
    expect(gitHunkAtLine(parsed, "additions", 2)?.index).toBe(0);
    expect(gitHunkAtLine(parsed, "deletions", 2)?.index).toBe(0);
    expect(gitHunkAtLine(parsed, "additions", 21)?.index).toBe(1);
    expect(gitHunkAtLine(parsed, "additions", 10)).toBeNull();
  });

  test("a hunk selects as its new-side range, or its old side when it only deletes", () => {
    expect(gitHunkSelection(parsed.hunks[0])).toEqual({ side: "additions", start: 1, end: 4 });
    const removal = parseGitPatch("@@ -5,2 +5,0 @@\n-a\n-b\n").hunks[0];
    expect(gitHunkSelection(removal)).toEqual({ side: "deletions", start: 5, end: 6 });
    expect(gitHunkChangeCounts(parsed.hunks[0])).toEqual({ additions: 1, deletions: 1 });
  });

  test("selected lines come back in hunk order", () => {
    expect(gitSelectedLines(parsed.hunks[0], "additions", 3, 1).map((line) => line.text)).toEqual(["one", "TWO", "three"]);
  });

  test("a binary patch has no hunks", () => {
    expect(parseGitPatch("diff --git a/x b/x\nBinary files a/x and b/x differ").binary).toBe(true);
  });
});

describe("ask the agent message", () => {
  const hunk = parseGitPatch(PATCH).hunks[0];
  test("names the file and line and quotes the diff", () => {
    const text = formatGitAskAgentMessage({
      path: "f.ts",
      side: "additions",
      start: 2,
      end: 2,
      lines: gitSelectedLines(hunk, "additions", 2, 2),
      comment: " Why uppercase? ",
      staged: false,
    });
    expect(text).toBe(["In the working-tree diff, f.ts:2:", "> + TWO", "", "Why uppercase?"].join("\n"));
  });
  test("a removed line says it is the old side, a range joins with a dash, long quotes are cut", () => {
    const removed = formatGitAskAgentMessage({
      path: "f.ts",
      side: "deletions",
      start: 2,
      end: 2,
      lines: gitSelectedLines(hunk, "deletions", 2, 2),
      comment: "Keep this",
      staged: true,
    });
    expect(removed.split("\n")[0]).toBe("In the staged diff, f.ts:2 (removed line, as it was before the change):");
    const many = Array.from({ length: 15 }, (_, i) => ({ kind: "+" as const, oldLine: null, newLine: i + 1, text: "x" }));
    const range = formatGitAskAgentMessage({ path: "g.ts", side: "additions", start: 1, end: 15, lines: many, comment: "?", staged: false });
    expect(range).toContain("g.ts:1-15:");
    expect(range).toContain("> … 3 more lines");
  });
});

import { splitGitPatchByFile } from "./gitDiffHunks";
import { gitSyncAction } from "./gitReviewModel";

describe("sync step", () => {
  const base: GitDiffStatusSnapshot = { available: true, clean: true, file_count: 0, staged_file_count: 0, unstaged_file_count: 0, untracked_file_count: 0, additions: 0, deletions: 0 };
  test("publish without an upstream, pull when behind, push when ahead, nothing when level", () => {
    expect(gitSyncAction({ ...base, has_commits: true, upstream: "" })).toEqual({ kind: "push", label: "Publish" });
    expect(gitSyncAction({ ...base, upstream: "origin/main", ahead: 2, behind: 1 })).toEqual({ kind: "pull", label: "Pull ↑2 ↓1" });
    expect(gitSyncAction({ ...base, upstream: "origin/main", ahead: 2, behind: 0 })).toEqual({ kind: "push", label: "Push ↑2" });
    expect(gitSyncAction({ ...base, upstream: "origin/main", ahead: 0, behind: 0 })).toBeNull();
    expect(gitSyncAction({ ...base, detached: true })).toBeNull();
    expect(gitSyncAction({ ...base, has_commits: false })).toBeNull();
  });
});

describe("a commit patch per file", () => {
  const patch = ["diff --git a/a.ts b/a.ts", "@@ -1 +1 @@", "-x", "+y", "diff --git a/dir/b b.ts b/dir/b b.ts", "@@ -1 +1 @@", "-p", "+q", ""].join("\n");
  test("pairs blocks with the daemon's files by position", () => {
    const map = splitGitPatchByFile(patch, [{ path: "a.ts" }, { path: "dir/b b.ts" }]);
    expect(map.get("a.ts")).toBe("diff --git a/a.ts b/a.ts\n@@ -1 +1 @@\n-x\n+y");
    expect(map.get("dir/b b.ts")?.startsWith("diff --git a/dir/b b.ts")).toBe(true);
  });
  test("falls back to the b/ path when the counts differ", () => {
    const map = splitGitPatchByFile(patch, [{ path: "a.ts" }]);
    expect([...map.keys()]).toEqual(["a.ts", "dir/b b.ts"]);
  });
});
