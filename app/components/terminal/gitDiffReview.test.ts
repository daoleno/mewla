import { describe, expect, test } from "bun:test";
import type {
  GitDiffFileContentPayload,
  GitDiffFileInfo,
  GitDiffPatchPayload,
} from "../../services/gitDiff";
import {
  GIT_DIFF_PATCH_LIMIT_BYTES,
  canExpandGitDiffFile,
  gitDiffExpansionSides,
  gitDiffPatchText,
  gitDiffSectionsForScope,
  gitDiffTreeDecoration,
  planGitDiffReview,
} from "./gitDiffReview";

const file = (extra: Partial<GitDiffFileInfo> = {}): GitDiffFileInfo => ({
  path: "src/a.ts",
  status: "modified",
  staged: false,
  unstaged: true,
  untracked: false,
  ...extra,
});

const payload: GitDiffPatchPayload = {
  repo_root: "/r",
  path: "src/a.ts",
  sections: [
    { scope: "staged", title: "Staged", patch: "diff --git a b\n@@ -1 +1 @@\n-x\n+y" },
    { scope: "unstaged", title: "Unstaged", patch: "diff --git a b\n@@ -1 +1 @@\n-y\n+z" },
    { scope: "untracked", title: "Untracked", patch: "" },
  ],
};

describe("Git diff scopes over daemon sections", () => {
  test("Working is unstaged plus untracked and Staged is staged only", () => {
    expect(gitDiffSectionsForScope(payload, "all").map((s) => s.scope)).toEqual(["staged", "unstaged", "untracked"]);
    expect(gitDiffSectionsForScope(payload, "working").map((s) => s.scope)).toEqual(["unstaged", "untracked"]);
    expect(gitDiffSectionsForScope(payload, "staged").map((s) => s.scope)).toEqual(["staged"]);
  });

  test("a file with several sections labels them, a single section does not", () => {
    const both = planGitDiffReview(payload, file({ staged: true }), "all");
    expect(both.kind === "sections" && both.sections.map((s) => s.label)).toEqual(["Staged", "Unstaged"]);
    const one = planGitDiffReview(payload, file({ staged: true }), "staged");
    expect(one.kind === "sections" && one.sections.map((s) => s.label)).toEqual([undefined]);
  });

  test("blank sections vanish and an empty scope is reported as empty", () => {
    expect(planGitDiffReview(payload, file(), "working").kind).toBe("sections");
    expect(planGitDiffReview({ ...payload, sections: [payload.sections[2]] }, file(), "all")).toEqual({ kind: "empty" });
  });

  test("patches past the limit are refused with their size", () => {
    const huge: GitDiffPatchPayload = {
      ...payload,
      sections: [{ scope: "unstaged", title: "Unstaged", patch: "x".repeat(GIT_DIFF_PATCH_LIMIT_BYTES + 1) }],
    };
    expect(planGitDiffReview(huge, file(), "all")).toEqual({ kind: "too-large", bytes: GIT_DIFF_PATCH_LIMIT_BYTES + 1 });
  });

  test("section ids stay unique even when a scope repeats across files", () => {
    const plan = planGitDiffReview(payload, file({ staged: true }), "all");
    expect(plan.kind === "sections" && new Set(plan.sections.map((s) => s.id)).size).toBe(2);
  });
});

describe("hunk expansion eligibility", () => {
  test("only a file with exactly one of staged or unstaged edits can expand", () => {
    expect(canExpandGitDiffFile(file())).toBe(true);
    expect(canExpandGitDiffFile(file({ staged: true, unstaged: false }))).toBe(true);
    expect(canExpandGitDiffFile(file({ staged: true, unstaged: true }))).toBe(false);
  });

  test("untracked, binary, added and deleted files never expand", () => {
    expect(canExpandGitDiffFile(file({ untracked: true, unstaged: false, status: "untracked" }))).toBe(false);
    expect(canExpandGitDiffFile(file({ binary: true }))).toBe(false);
    expect(canExpandGitDiffFile(file({ status: "added" }))).toBe(false);
    expect(canExpandGitDiffFile(file({ status: "deleted" }))).toBe(false);
  });

  test("a rename expands against the old path's content", () => {
    expect(canExpandGitDiffFile(file({ status: "renamed", old_path: "old.ts" }))).toBe(true);
  });

  const side = (extra: Partial<GitDiffFileContentPayload["base"]> = {}) => ({
    label: "x",
    exists: true,
    byte_count: 1,
    line_count: 1,
    content: "a\n",
    ...extra,
  });

  test("whole text sides are returned and anything partial is refused", () => {
    const ok = { repo_root: "/r", path: "p", base: side({ content: "old\n" }), current: side({ content: "new\n" }) };
    expect(gitDiffExpansionSides(ok, file({ old_path: "o.ts" }))).toEqual({ oldContents: "old\n", newContents: "new\n", oldName: "o.ts" });
    expect(gitDiffExpansionSides({ ...ok, current: side({ truncated: true }) }, file())).toBeNull();
    expect(gitDiffExpansionSides({ ...ok, base: side({ binary: true }) }, file())).toBeNull();
    expect(gitDiffExpansionSides({ ...ok, base: side({ exists: false }) }, file())).toBeNull();
  });
});

describe("Git diff text and tree rows", () => {
  test("copying a scope joins its patches the way git printed them", () => {
    expect(gitDiffPatchText(payload, "staged")).toBe(`${payload.sections[0].patch}\n`);
    expect(gitDiffPatchText(payload, "all")).toBe(`${payload.sections[0].patch}\n${payload.sections[1].patch}\n`);
  });

  test("a binary file shows Binary instead of counts", () => {
    expect(gitDiffTreeDecoration(file({ binary: true }), [3, 4])).toEqual({ additions: 0, deletions: 0, binary: true });
    expect(gitDiffTreeDecoration(file(), [3, 4])).toEqual({ additions: 3, deletions: 4 });
  });
});
