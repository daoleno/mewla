import { codedRequest, type Daemon } from "../more/daemon";
import type {
  GitBranchesPayload,
  GitCommitDetail,
  GitDiffFileContentPayload,
  GitDiffPatchPayload,
  GitDiffStatusSnapshot,
  GitLogPayload,
  GitRepoFileContentPayload,
  GitRepoPathsPayload,
} from "./gitDiff";

/**
 * Git review asks the current computer about one Session's repository: the
 * Session (`targetId`) and the folder it reported (`cwd`). A mutation also names
 * the repository the person is looking at (`repoRoot`); the computer refuses it
 * when the terminal has since moved to another one. Git's own words pass through.
 */

export interface GitTarget {
  targetId: string;
  cwd: string;
}

const DISCONNECTED = "Server disconnected. Reconnect to check the result.";

function body(target: GitTarget, extra: Record<string, unknown> = {}): Record<string, unknown> {
  return { target_id: target.targetId, cwd: target.cwd, ...extra };
}

async function read<T>(daemon: Daemon, type: string, fields: Record<string, unknown>, field: string, timeoutMs: number, what: string): Promise<T> {
  const frame = await codedRequest<Record<string, unknown>>(daemon, type, fields, {
    reply: type,
    timeoutMs,
    timeout: `Timed out while loading ${what}.`,
    failure: `Failed to load ${what}.`,
    disconnected: DISCONNECTED,
  });
  return frame[field] as T;
}

/** A repository that answers nothing yet: no status at all. */
const NO_STATUS: GitDiffStatusSnapshot = {
  available: false,
  clean: true,
  file_count: 0,
  staged_file_count: 0,
  unstaged_file_count: 0,
  untracked_file_count: 0,
  additions: 0,
  deletions: 0,
  files: [],
};

export async function getGitStatus(daemon: Daemon, target: GitTarget): Promise<GitDiffStatusSnapshot> {
  return (await read<GitDiffStatusSnapshot | undefined>(daemon, "git_diff_status", body(target), "status", 10_000, "git diff status")) ?? NO_STATUS;
}

/** One file's whole patch, not a page: a large one takes a while. */
export function getGitPatch(daemon: Daemon, target: GitTarget, path: string): Promise<GitDiffPatchPayload> {
  return read(daemon, "git_diff_patch", body(target, { path }), "patch", 30_000, "git diff patch");
}

export function getGitFileContent(daemon: Daemon, target: GitTarget, path: string): Promise<GitDiffFileContentPayload> {
  return read(daemon, "git_diff_file_content", body(target, { path }), "content", 10_000, "git diff file content");
}

export function getGitRepoPaths(daemon: Daemon, target: GitTarget): Promise<GitRepoPathsPayload> {
  return read(daemon, "git_repo_paths", body(target), "browser", 15_000, "repository files");
}

export function getGitRepoFile(daemon: Daemon, target: GitTarget, path: string): Promise<GitRepoFileContentPayload> {
  return read(daemon, "git_repo_file_content", body(target, { path }), "content", 10_000, "repository file");
}

export function getGitBranches(daemon: Daemon, target: GitTarget): Promise<GitBranchesPayload> {
  return read(daemon, "git_branches", body(target), "branches", 15_000, "branches");
}

export const GIT_LOG_PAGE = 40;

export function getGitLog(daemon: Daemon, target: GitTarget, skip: number, limit = GIT_LOG_PAGE): Promise<GitLogPayload> {
  return read(daemon, "git_log", body(target, { limit, skip }), "log", 15_000, "history");
}

export async function getGitCommit(daemon: Daemon, target: GitTarget, sha: string): Promise<GitCommitDetail> {
  const frame = await codedRequest<Record<string, unknown>>(daemon, "git_show", body(target, { sha }), {
    reply: "git_show",
    timeoutMs: 20_000,
    timeout: "Timed out while loading the commit.",
    failure: "Failed to load the commit.",
    disconnected: DISCONNECTED,
  });
  return frame.commit as GitCommitDetail;
}

export type GitAction = "stage" | "unstage" | "discard" | "discard_undo" | "hunk" | "commit" | "push" | "pull" | "fetch" | "switch";

/** Push, pull and fetch talk to a remote; they get longer. */
const SLOW: ReadonlySet<GitAction> = new Set(["push", "pull", "fetch"]);

/** One change in the Session's repository. Rejects with Git's own message. */
export async function runGitAction<T = Record<string, unknown>>(
  daemon: Daemon,
  target: GitTarget,
  repoRoot: string,
  action: GitAction,
  fields: Record<string, unknown> = {},
): Promise<T> {
  const frame = await codedRequest<{ action?: string; result?: T }>(daemon, `git_${action}`, body(target, { repo_root: repoRoot, ...fields }), {
    reply: "git_action",
    timeoutMs: SLOW.has(action) ? 90_000 : 40_000,
    timeout: `Git ${action} timed out.`,
    failure: `Git ${action} failed.`,
    disconnected: DISCONNECTED,
  });
  if (frame.action !== undefined && frame.action !== action) throw new Error(`Git ${action} failed.`);
  return (frame.result ?? {}) as T;
}
