import { describe, expect, test } from "bun:test";
import {
  gitDiffStatusMark,
  gitDiffStatusWord,
  gitDiffTone,
  formatGitDelta,
  splitGitDiffPath,
} from "./gitDiffPresentation";
import type { GitDiffFileInfo } from "./gitDiff";

function file(overrides: Partial<GitDiffFileInfo>): GitDiffFileInfo {
  return {
    path: "src/app.ts",
    status: "modified",
    staged: false,
    unstaged: true,
    untracked: false,
    ...overrides,
  };
}

describe("Git diff file presentation", () => {
  test("splits top-level and nested paths into name and directory", () => {
    expect(splitGitDiffPath("README.md")).toEqual({
      directory: "",
      name: "README.md",
    });
    expect(splitGitDiffPath("src/terminal/app.ts")).toEqual({
      directory: "src/terminal/",
      name: "app.ts",
    });
  });
  test("status maps to a tone, a one-character mark and a spoken word", () => {
    expect(gitDiffTone(file({ status: "added" }))).toBe("added");
    expect(gitDiffStatusMark("added")).toBe("+");
    expect(gitDiffTone(file({ status: "untracked", untracked: true }))).toBe("untracked");
    expect(gitDiffStatusMark("untracked")).toBe("?");
    expect(gitDiffTone(file({ status: "copied" }))).toBe("renamed");
    expect(gitDiffStatusWord(gitDiffTone(file({ status: "deleted" })))).toBe("Deleted");
    expect(gitDiffStatusWord(gitDiffTone(file({ status: "weird" })))).toBe("Modified");
  });
  test("binary overrides the status", () => {
    expect(gitDiffTone(file({ status: "modified", binary: true }))).toBe("binary");
  });
});

describe("Git diff counts", () => {
  test("counts abbreviate", () => {
    expect(formatGitDelta(999)).toBe("999");
    expect(formatGitDelta(1200)).toBe("1.2k");
    expect(formatGitDelta(12400)).toBe("12k");
    expect(formatGitDelta(-1)).toBe("0");
  });
});
