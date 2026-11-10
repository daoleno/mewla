export type GitDiffViewMode = "changes" | "files";

export interface GitDiffBackState {
  view: GitDiffViewMode;
  /** A changed file's diff is open over the list. */
  hasOpenDiff: boolean;
  /** History tab: a commit is open, and maybe one of its file diffs. */
  hasOpenCommit: boolean;
  hasBrowserFile: boolean;
  /** Where the currently open working file was opened from. */
  fileOrigin: GitDiffViewMode;
  searchOpen: boolean;
}

export type GitDiffBackAction =
  | "close"
  | "close-search"
  | "close-diff"
  | "close-commit"
  | "close-browser-file-to-reader"
  | "close-browser-file-to-browser"
  | "browser-to-changes";

/**
 * What Android hardware back (or a header back control) unwinds. A child state
 * never closes the whole sheet; only the top level does. Closing a working
 * file returns to where it was opened from, so every Back control agrees. The
 * Files tree has no folder levels to climb, so Back there returns to the
 * changes list.
 */
export function resolveGitDiffBack(state: GitDiffBackState): GitDiffBackAction {
  if (state.view === "files") {
    if (!state.hasBrowserFile) return "browser-to-changes";
    return state.fileOrigin === "changes" ? "close-browser-file-to-reader" : "close-browser-file-to-browser";
  }
  if (state.hasOpenDiff) return "close-diff";
  if (state.hasOpenCommit) return "close-commit";
  if (state.searchOpen) return "close-search";
  return "close";
}
