/**
 * Git review of one Session's repository: the Session (`targetId`) and the folder
 * it reported (`cwd`). A Session opens `{ name: "git", targetId, cwd, title }`; the
 * rest are the pages it pushes.
 */
export type GitPage =
  | { name: "git"; targetId: string; cwd: string; title?: string }
  /** One changed file's diff; `entry` is its review key (`staged:path` or `changes:path`). */
  | { name: "git-diff"; targetId: string; cwd: string; entry: string }
  | { name: "git-commit"; targetId: string; cwd: string; sha: string }
  | { name: "git-commit-file"; targetId: string; cwd: string; sha: string; path: string }
  | { name: "git-files"; targetId: string; cwd: string }
  | { name: "git-file"; targetId: string; cwd: string; path: string };

/**
 * Git review of a Session's repository, for the Session's top-bar Git button and its menu's
 * "Open Git diff": `nav.push(gitReviewRoute(session.id, session.cwd, title))`. The chip's
 * label is `buildGitDiffChipLabel` (core/git/gitDiff.ts) over `getGitStatus` (core/git/requests.ts).
 */
export function gitReviewRoute(targetId: string, cwd: string, title?: string): GitPage {
  return title ? { name: "git", targetId, cwd, title } : { name: "git", targetId, cwd };
}
