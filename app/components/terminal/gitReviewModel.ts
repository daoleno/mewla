import type { GitDiffFileInfo, GitDiffStatusSnapshot } from "../../services/gitDiff";

/**
 * Git review lists changes the way the index holds them: what is staged, and
 * what is not (changed tracked files and untracked ones). A file edited both
 * ways appears once in each group, so every row is one comparison.
 */
export type GitReviewGroup = "staged" | "changes";

export interface GitReviewEntry {
  /** Unique across groups: `staged:path` or `changes:path`. */
  key: string;
  group: GitReviewGroup;
  file: GitDiffFileInfo;
  additions: number;
  deletions: number;
}

export interface GitReviewGroups {
  staged: GitReviewEntry[];
  changes: GitReviewEntry[];
}

export function gitReviewEntryKey(group: GitReviewGroup, path: string): string {
  return `${group}:${path}`;
}

export function buildGitReviewGroups(files: readonly GitDiffFileInfo[]): GitReviewGroups {
  const staged: GitReviewEntry[] = [];
  const changes: GitReviewEntry[] = [];
  for (const file of files) {
    if (file.staged) {
      staged.push({
        key: gitReviewEntryKey("staged", file.path),
        group: "staged",
        file,
        additions: file.staged_additions ?? 0,
        deletions: file.staged_deletions ?? 0,
      });
    }
    if (file.unstaged || file.untracked) {
      changes.push({
        key: gitReviewEntryKey("changes", file.path),
        group: "changes",
        file,
        additions: file.working_additions ?? 0,
        deletions: file.working_deletions ?? 0,
      });
    }
  }
  return { staged, changes };
}

/** Reading order: staged first, then changes, each sorted by path. */
export function gitReviewOrder(groups: GitReviewGroups): GitReviewEntry[] {
  return [...groups.staged, ...groups.changes];
}

/** The neighbour of an entry in reading order, or null at either end. */
export function adjacentGitReviewEntry(
  order: readonly GitReviewEntry[],
  key: string,
  direction: "next" | "previous",
): GitReviewEntry | null {
  const index = order.findIndex((entry) => entry.key === key);
  if (index === -1) return null;
  return order[index + (direction === "next" ? 1 : -1)] ?? null;
}

export function sumGitReviewEntries(entries: readonly GitReviewEntry[]) {
  let additions = 0;
  let deletions = 0;
  for (const entry of entries) {
    if (entry.file.binary) continue;
    additions += entry.additions;
    deletions += entry.deletions;
  }
  return { additions, deletions };
}

/** `+416 −10 · 14 files`: the one summary of everything that changed. */
export function gitReviewSummary(snapshot: GitDiffStatusSnapshot | null): string {
  if (!snapshot?.available || snapshot.clean) return "";
  const files = `${snapshot.file_count} ${snapshot.file_count === 1 ? "file" : "files"}`;
  return `+${snapshot.additions} −${snapshot.deletions} · ${files}`;
}

/** `↑2 ↓1`, or an empty string when level with the upstream. */
export function gitReviewSync(snapshot: Pick<GitDiffStatusSnapshot, "ahead" | "behind"> | null): string {
  const ahead = snapshot?.ahead ?? 0;
  const behind = snapshot?.behind ?? 0;
  return [ahead > 0 ? `↑${ahead}` : "", behind > 0 ? `↓${behind}` : ""].filter(Boolean).join(" ");
}

export type GitReviewRow =
  | { kind: "dir"; key: string; path: string; label: string; depth: number; count: number; collapsed: boolean }
  | { kind: "file"; key: string; entry: GitReviewEntry; depth: number };

export function matchesGitReviewQuery(path: string, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  const haystack = path.toLowerCase();
  return needle.split(/\s+/).every((part) => haystack.includes(part));
}

interface TreeNode {
  name: string;
  path: string;
  dirs: Map<string, TreeNode>;
  files: GitReviewEntry[];
  count: number;
}

/**
 * The rows of one group. List mode is the entries as they come; tree mode
 * nests them by folder, joining a chain of single-folder levels into one row
 * (`app/components/screen`) so a deep path costs one row, not four.
 */
export function buildGitReviewRows(
  entries: readonly GitReviewEntry[],
  options: { tree: boolean; collapsed: ReadonlySet<string>; query: string; scope: string },
): GitReviewRow[] {
  const visible = entries.filter((entry) => matchesGitReviewQuery(entry.file.path, options.query));
  if (!options.tree) {
    return visible.map((entry) => ({ kind: "file", key: entry.key, entry, depth: 0 }));
  }
  const root: TreeNode = { name: "", path: "", dirs: new Map(), files: [], count: 0 };
  for (const entry of visible) {
    const parts = entry.file.path.split("/");
    let node = root;
    node.count += 1;
    for (const part of parts.slice(0, -1)) {
      let child = node.dirs.get(part);
      if (!child) {
        child = { name: part, path: node.path ? `${node.path}/${part}` : part, dirs: new Map(), files: [], count: 0 };
        node.dirs.set(part, child);
      }
      child.count += 1;
      node = child;
    }
    node.files.push(entry);
  }
  const rows: GitReviewRow[] = [];
  const walk = (node: TreeNode, depth: number) => {
    const dirs = [...node.dirs.values()].sort((a, b) => a.name.localeCompare(b.name));
    for (let dir of dirs) {
      let label = dir.name;
      while (dir.files.length === 0 && dir.dirs.size === 1) {
        dir = [...dir.dirs.values()][0];
        label = `${label}/${dir.name}`;
      }
      const key = `${options.scope}:dir:${dir.path}`;
      const collapsed = options.collapsed.has(key);
      rows.push({ kind: "dir", key, path: dir.path, label, depth, count: dir.count, collapsed });
      if (!collapsed) walk(dir, depth + 1);
    }
    for (const entry of node.files) rows.push({ kind: "file", key: entry.key, entry, depth });
  };
  walk(root, 0);
  return rows;
}

export function splitGitReviewPath(path: string): { directory: string; name: string } {
  const index = path.lastIndexOf("/");
  return index === -1 ? { directory: "", name: path } : { directory: path.slice(0, index + 1), name: path.slice(index + 1) };
}

/** Every path of the given entries, plus rename sources, for a stage or unstage request. */
export function gitReviewPaths(entries: readonly GitReviewEntry[]): string[] {
  const paths = new Set<string>();
  for (const entry of entries) {
    paths.add(entry.file.path);
    if (entry.file.old_path) paths.add(entry.file.old_path);
  }
  return [...paths];
}

export interface GitSyncAction {
  kind: "push" | "pull";
  label: string;
}

/** The one sensible next sync step for the branch, or null when it is level or cannot sync. */
export function gitSyncAction(snapshot: GitDiffStatusSnapshot | null): GitSyncAction | null {
  if (!snapshot?.available || snapshot.detached || snapshot.has_commits === false) return null;
  const ahead = snapshot.ahead ?? 0;
  const behind = snapshot.behind ?? 0;
  if (!snapshot.upstream) return { kind: "push", label: "Publish" };
  if (behind > 0) return { kind: "pull", label: `Pull ${gitReviewSync(snapshot)}` };
  if (ahead > 0) return { kind: "push", label: `Push ${gitReviewSync(snapshot)}` };
  return null;
}
