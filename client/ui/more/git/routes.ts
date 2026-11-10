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
