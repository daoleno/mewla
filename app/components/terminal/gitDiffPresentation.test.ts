import { describe, expect, test } from "bun:test";
import {
  gitDiffStatusMark,
  gitDiffStatusWord,
  gitDiffTone,
  describeGitDiffTopBarButton,
  formatGitDelta,
  splitGitDiffPath,
} from "./gitDiffPresentation";
import type { GitDiffFileInfo } from "../../services/gitDiff";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { TerminalGitDiffSummary } from "./useTerminalGitDiff";

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

describe("Git diff top-bar button", () => {
  const chrome = {
    accent: "#accent",
    danger: "#danger",
    text: "#text",
    textMuted: "#muted",
    textSubtle: "#subtle",
  } as TerminalThemeChrome;
  const terminalTheme = { green: "#green", red: "#red" };
  function summary(overrides: Partial<TerminalGitDiffSummary>): TerminalGitDiffSummary {
    return {
      label: "2 files changed",
      tone: "dirty",
      additions: 12,
      deletions: 3,
      fileCount: 2,
      showStats: true,
      ...overrides,
    };
  }
  function button(s: TerminalGitDiffSummary | null, disabled = false) {
    return describeGitDiffTopBarButton({ chrome, disabled, summary: s, terminalTheme });
  }

  test("dirty shows ink and coloured counts", () => {
    expect(button(summary({}))).toMatchObject({
      iconColor: "#accent",
      additionsText: "+12",
      additionsColor: "#green",
      deletionsText: "\u22123",
      deletionsColor: "#red",
      markColor: undefined,
      accessibilityLabel: "Open Git diff, 2 files changed, +12 -3",
    });
  });
  test("a side with no lines is left out", () => {
    const added = button(summary({ deletions: 0 }));
    expect(added.additionsText).toBe("+12");
    expect(added.deletionsText).toBeUndefined();
    const removed = button(summary({ additions: 0 }));
    expect(removed.additionsText).toBeUndefined();
    expect(removed.deletionsText).toBe("\u22123");
  });
  test("dirty without countable lines gets a dot instead", () => {
    const dot = button(summary({ additions: 0, deletions: 0 }));
    expect(dot).toMatchObject({ iconColor: "#accent", markColor: "#accent" });
    expect(dot.additionsText).toBeUndefined();
    expect(dot.deletionsText).toBeUndefined();
  });
  test("clean and loading stay muted with no counts", () => {
    for (const tone of ["clean", "loading"] as const) {
      const quiet = button(summary({ tone, additions: 0, deletions: 0 }));
      expect(quiet.iconColor).toBe("#muted");
      expect(quiet.additionsText).toBeUndefined();
      expect(quiet.markColor).toBeUndefined();
    }
  });
  test("error is danger; disabled and not-a-repo stay subtle", () => {
    expect(button(summary({ tone: "error" })).iconColor).toBe("#danger");
    const disabled = button(summary({}), true);
    expect(disabled.iconColor).toBe("#subtle");
    expect(disabled.additionsText).toBeUndefined();
    expect(button(null, true).iconColor).toBe("#subtle");
    expect(button(null).iconColor).toBe("#muted");
  });
  test("counts abbreviate", () => {
    expect(formatGitDelta(999)).toBe("999");
    expect(formatGitDelta(1200)).toBe("1.2k");
    expect(formatGitDelta(12400)).toBe("12k");
    expect(formatGitDelta(-1)).toBe("0");
    expect(button(summary({ additions: 1200, deletions: 12400 }))).toMatchObject({
      additionsText: "+1.2k",
      deletionsText: "\u221212k",
    });
  });
});
