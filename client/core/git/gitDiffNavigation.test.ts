import { describe, expect, test } from "bun:test";
import { resolveGitDiffBack, type GitDiffBackState } from "./gitDiffNavigation";

const state = (extra: Partial<GitDiffBackState> = {}): GitDiffBackState => ({
  view: "changes",
  hasOpenDiff: false,
  hasOpenCommit: false,
  hasBrowserFile: false,
  fileOrigin: "files",
  searchOpen: false,
  ...extra,
});

describe("Git review back stack", () => {
  test("only the top level closes the sheet", () => {
    expect(resolveGitDiffBack(state())).toBe("close");
    expect(resolveGitDiffBack(state({ hasOpenDiff: true }))).toBe("close-diff");
    expect(resolveGitDiffBack(state({ hasOpenCommit: true }))).toBe("close-commit");
  });
  test("an open diff unwinds before its commit, and the search bar before closing", () => {
    expect(resolveGitDiffBack(state({ hasOpenCommit: true, hasOpenDiff: true }))).toBe("close-diff");
    expect(resolveGitDiffBack(state({ searchOpen: true }))).toBe("close-search");
  });
  test("the file browser returns to the changes, and a working file to where it was opened", () => {
    expect(resolveGitDiffBack(state({ view: "files" }))).toBe("browser-to-changes");
    expect(resolveGitDiffBack(state({ view: "files", hasBrowserFile: true, fileOrigin: "changes" }))).toBe("close-browser-file-to-reader");
    expect(resolveGitDiffBack(state({ view: "files", hasBrowserFile: true, fileOrigin: "files" }))).toBe("close-browser-file-to-browser");
  });
});
