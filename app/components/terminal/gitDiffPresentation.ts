import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { GitDiffFileInfo } from "../../services/gitDiff";
import type { TerminalTopBarGitDiffPresentation } from "./TerminalTopBar";
import type { TerminalGitDiffSummary } from "./useTerminalGitDiff";

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

/**
 * Semantic ink for a status tone. Added and deleted keep the terminal's change
 * colours; a modified or renamed file is ink, since amber reads as Warning and
 * blue as Running in Seal & Slip.
 */
export function gitDiffToneColor(
  tone: GitDiffStatusTone,
  palette: { green: string; red: string },
  muted: string,
  ink: string,
): string {
  switch (tone) {
    case "added":
      return palette.green;
    case "deleted":
    case "conflict":
      return palette.red;
    case "renamed":
    case "modified":
      return ink;
    case "untracked":
    case "binary":
      return muted;
  }
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

/**
 * The top-bar Git button. Clean and unknown stay muted; uncommitted work is
 * the one state worth a glance, so it gets full ink, its `+12 \u22123` counts in
 * the terminal's change colours (a side with no lines is left out), and a dot
 * when it has no countable lines (untracked or binary files only).
 */
export function describeGitDiffTopBarButton({
  chrome,
  disabled,
  summary,
  terminalTheme,
}: {
  chrome: TerminalThemeChrome;
  disabled: boolean;
  summary: TerminalGitDiffSummary | null;
  terminalTheme: Pick<TerminalThemePalette, "green" | "red">;
}): TerminalTopBarGitDiffPresentation {
  if (!summary) {
    return {
      accessibilityLabel: "Open Git diff",
      backgroundColor: "transparent",
      iconColor: disabled ? chrome.textSubtle : chrome.textMuted,
    };
  }

  const statsLabel = summary.showStats
    ? `+${formatGitDelta(summary.additions)} -${formatGitDelta(summary.deletions)}`
    : "";
  const base = {
    accessibilityLabel: ["Open Git diff", summary.label, statsLabel]
      .filter(Boolean)
      .join(", "),
    backgroundColor: "transparent",
  };
  if (disabled) {
    return { ...base, iconColor: chrome.textSubtle };
  }
  if (summary.tone === "error") {
    return { ...base, iconColor: chrome.danger };
  }
  if (summary.tone !== "dirty") {
    return { ...base, iconColor: chrome.textMuted };
  }

  const additions = summary.showStats && summary.additions > 0;
  const deletions = summary.showStats && summary.deletions > 0;
  return {
    ...base,
    iconColor: chrome.accent,
    additionsText: additions ? `+${formatGitDelta(summary.additions)}` : undefined,
    additionsColor: additions ? terminalTheme.green : undefined,
    deletionsText: deletions
      ? `\u2212${formatGitDelta(summary.deletions)}`
      : undefined,
    deletionsColor: deletions ? terminalTheme.red : undefined,
    markColor: additions || deletions ? undefined : chrome.accent,
  };
}
