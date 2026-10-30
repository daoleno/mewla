import { describe, expect, test } from "bun:test";
import {
  describeGitDiffFile,
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
  test("status maps to a glyph tone and scope wording", () => {
    expect(describeGitDiffFile(file({ status: "added" }), "all")).toMatchObject({
      statusLabel: "Added",
      scopeLabel: "Unstaged",
      icon: "add-circle",
      tone: "added",
    });
    expect(
      describeGitDiffFile(file({ status: "untracked", untracked: true }), "all"),
    ).toMatchObject({ scopeLabel: "Untracked", tone: "untracked" });
    expect(
      describeGitDiffFile(
        file({ status: "deleted", staged: true, unstaged: false }),
        "staged",
      ),
    ).toMatchObject({ scopeLabel: "Staged", tone: "deleted" });
  });
  test("rename keeps the destination name and the source", () => {
    const presentation = describeGitDiffFile(
      file({
        status: "renamed",
        old_path: "src/old-app.ts",
        path: "src/new/app.ts",
      }),
      "all",
    );
    expect(presentation.name).toBe("app.ts");
    expect(presentation.directory).toBe("src/new/");
    expect(presentation.oldName).toBe("old-app.ts");
    expect(presentation.oldDirectory).toBe("src/");
  });
  test("binary overrides code stats with a binary presentation", () => {
    const presentation = describeGitDiffFile(
      file({ binary: true, additions: 0, deletions: 0 }),
      "all",
    );
    expect(presentation.binary).toBe(true);
    expect(presentation.icon).toBe("cube");
    expect(presentation.tone).toBe("binary");
  });
  test("scope counts use the selected comparison rather than a sum", () => {
    const both = file({
      staged: true,
      unstaged: true,
      additions: 5,
      deletions: 3,
      staged_additions: 2,
      staged_deletions: 1,
      working_additions: 3,
      working_deletions: 2,
    });
    expect(
      describeGitDiffFile(both, "staged"),
    ).toMatchObject({ additions: 2, deletions: 1 });
    expect(
      describeGitDiffFile(both, "working"),
    ).toMatchObject({ additions: 3, deletions: 2 });
    expect(describeGitDiffFile(both, "all")).toMatchObject({
      additions: 5,
      deletions: 3,
    });
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
