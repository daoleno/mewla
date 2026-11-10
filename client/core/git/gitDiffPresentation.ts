import type { GitDiffFileInfo } from "./gitDiff";

export type GitDiffStatusTone =
  | "added"
  | "deleted"
  | "renamed"
  | "modified"
  | "conflict"
  | "untracked"
  | "binary";

const STATUS_TONES: Record<string, GitDiffStatusTone> = {
  added: "added",
  deleted: "deleted",
  renamed: "renamed",
  copied: "renamed",
  conflict: "conflict",
  untracked: "untracked",
  modified: "modified",
  changed: "modified",
};

const STATUS_MARKS: Record<GitDiffStatusTone, string> = {
  added: "+",
  deleted: "\u2212",
  renamed: "\u2192",
  modified: "\u2022",
  conflict: "!",
  untracked: "?",
  binary: "\u25a0",
};

const STATUS_WORDS: Record<GitDiffStatusTone, string> = {
  added: "Added",
  deleted: "Deleted",
  renamed: "Renamed",
  modified: "Modified",
  conflict: "Conflict",
  untracked: "Untracked",
  binary: "Binary",
};

export function gitDiffTone(file: Pick<GitDiffFileInfo, "status" | "binary">): GitDiffStatusTone {
  return file.binary ? "binary" : STATUS_TONES[file.status] ?? "modified";
}

/** The one-character mark a row shows for a tone. */
export function gitDiffStatusMark(tone: GitDiffStatusTone): string {
  return STATUS_MARKS[tone];
}

/** The status in words, for screen readers. */
export function gitDiffStatusWord(tone: GitDiffStatusTone): string {
  return STATUS_WORDS[tone];
}

export function splitGitDiffPath(path: string): {
  directory: string;
  name: string;
} {
  const index = path.lastIndexOf("/");
  if (index === -1) {
    return { directory: "", name: path };
  }
  return { directory: path.slice(0, index + 1), name: path.slice(index + 1) };
}

/** `999`, `1.2k`, `12k`: change counts that stay narrow in the top bar. */
export function formatGitDelta(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "0";
  }
  if (value < 1000) {
    return String(value);
  }
  if (value < 10000) {
    return `${(value / 1000).toFixed(1).replace(/\.0$/, "")}k`;
  }
  return `${Math.round(value / 1000)}k`;
}
