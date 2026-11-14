export type GitDiffUnavailableReason = "no_cwd" | "not_git_repo";

export type GitDiffFileStatus =
  | "added"
  | "changed"
  | "conflict"
  | "copied"
  | "deleted"
  | "modified"
  | "renamed"
  | "untracked";

export interface GitDiffFileInfo {
  path: string;
  old_path?: string;
  status: GitDiffFileStatus | string;
  staged: boolean;
  unstaged: boolean;
  untracked: boolean;
  additions?: number;
  deletions?: number;
  binary?: boolean;
  /** A new folder reported whole ("app/"), as `git status` shows it: no lines, no diff of its own. */
  directory?: boolean;
  staged_additions?: number;
  staged_deletions?: number;
  working_additions?: number;
  working_deletions?: number;
}

export type GitDiffScope = "all" | "working" | "staged";

export function filterGitDiffFiles(files: GitDiffFileInfo[], scope: GitDiffScope) {
  return files.filter(
    (file) =>
      scope === "all" ||
      (scope === "staged" ? file.staged : file.unstaged || file.untracked),
  );
}

export function gitDiffCounts(file: GitDiffFileInfo, scope: GitDiffScope) {
  return scope === "staged"
    ? [file.staged_additions ?? 0, file.staged_deletions ?? 0]
    : scope === "working"
      ? [file.working_additions ?? 0, file.working_deletions ?? 0]
      : [file.additions ?? 0, file.deletions ?? 0];
}

export interface GitDiffStatusSnapshot {
  available: boolean;
  reason?: GitDiffUnavailableReason | string;
  repo_root?: string;
  repo_name?: string;
  branch?: string;
  clean: boolean;
  file_count: number;
  staged_file_count: number;
  unstaged_file_count: number;
  untracked_file_count: number;
  additions: number;
  deletions: number;
  files?: GitDiffFileInfo[];
  /** `files` holds only the first of `file_count` (the computer lists at most 1000); the counts cover them all. */
  truncated?: boolean;
  /** Tracking branch, e.g. `origin/main`; empty when none is set. */
  upstream?: string;
  ahead?: number;
  behind?: number;
  detached?: boolean;
  /** False in a repository with no commit yet. */
  has_commits?: boolean;
  /** A merge, rebase, cherry-pick or revert is in progress. */
  operation?: "" | "merge" | "rebase" | "cherry-pick" | "revert" | string;
}

export interface GitDiffPatchSection {
  scope: "staged" | "unstaged" | "untracked" | string;
  title: string;
  patch: string;
}

export interface GitDiffPatchPayload {
  repo_root: string;
  path: string;
  sections: GitDiffPatchSection[];
}

export interface GitDiffContentSnapshot {
  label: string;
  exists: boolean;
  binary?: boolean;
  truncated?: boolean;
  reason?: string;
  byte_count: number;
  line_count: number;
  content?: string;
}

export interface GitDiffFileContentPayload {
  repo_root: string;
  path: string;
  /** The comparison the sides are of (`gitDiffContentScope`); none: HEAD → working tree. */
  scope?: "staged" | "unstaged";
  current: GitDiffContentSnapshot;
  base: GitDiffContentSnapshot;
}

export interface GitRepoPathsPayload {
  repo_root: string;
  /** Every tracked or untracked-but-not-ignored file, sorted, repo-relative. */
  paths: string[];
  /** The daemon capped the list; the tree shows a prefix of the repository. */
  truncated: boolean;
}

export interface GitRepoFileContentPayload {
  repo_root: string;
  path: string;
  snapshot: GitDiffContentSnapshot;
}

export function buildGitDiffChipLabel(
  snapshot: GitDiffStatusSnapshot | null,
  loading: boolean,
): string {
  if (loading && !snapshot) {
    return "Checking repo…";
  }
  if (!snapshot?.available) {
    return "Git diff";
  }
  if (snapshot.clean) {
    return snapshot.branch || "Git";
  }

  const fileLabel =
    snapshot.file_count === 1 ? "1 file" : `${snapshot.file_count} files`;
  if (snapshot.additions > 0 || snapshot.deletions > 0) {
    return `${fileLabel}  +${snapshot.additions}  -${snapshot.deletions}`;
  }
  return fileLabel;
}

export function describeGitDiffScope(file: GitDiffFileInfo): string {
  if (file.untracked) return "Untracked";
  if (file.staged && file.unstaged) return "Staged + unstaged";
  if (file.staged) return "Staged";
  if (file.unstaged) return "Unstaged";
  return "Changed";
}

export interface GitBranchInfo {
  name: string;
  current: boolean;
  /** Only on the remote; switching creates a local tracking branch. */
  remote: boolean;
  upstream?: string;
  ahead?: number;
  behind?: number;
  subject?: string;
  /** Unix seconds of the last commit. */
  date?: number;
}

export interface GitBranchesPayload {
  current: string;
  detached: boolean;
  branches: GitBranchInfo[];
}

export interface GitCommitSummary {
  sha: string;
  short: string;
  subject: string;
  author: string;
  /** Unix seconds. */
  date: number;
  refs?: string[];
  parents?: number;
}

export interface GitLogPayload {
  commits: GitCommitSummary[];
  has_more: boolean;
}

export interface GitCommitFile {
  path: string;
  old_path?: string;
  status: GitDiffFileStatus | string;
  additions: number;
  deletions: number;
  binary?: boolean;
}

export interface GitCommitDetail extends GitCommitSummary {
  body: string;
  files: GitCommitFile[];
  patch: string;
  truncated?: boolean;
}

/** What a discard left behind, kept by the app so one tap can put it back. */
export interface GitDiscardUndo {
  path: string;
  blob: string;
  after: string;
  mode: string;
  staged: boolean;
}

export type GitHunkAction = "stage" | "unstage" | "discard" | "apply";
