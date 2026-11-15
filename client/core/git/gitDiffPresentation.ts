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
