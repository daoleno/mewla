import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { IconName } from "../icons/Icon";
import {
  describeGitDiffScope,
  gitDiffCounts,
  type GitDiffFileInfo,
  type GitDiffScope,
} from "../../services/gitDiff";
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

export interface GitDiffFilePresentation {
  name: string;
  directory: string;
  oldName: string | null;
  oldDirectory: string | null;
  statusLabel: string;
  scopeLabel: string;
  icon: IconName;
  /** One-letter status mark shown in the row's tinted tile (M, A, D, R...). */
  glyph: string;
  tone: GitDiffStatusTone;
  additions: number;
  deletions: number;
  binary: boolean;
}

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

const TONE_ICONS: Record<GitDiffStatusTone, IconName> = {
  added: "add-circle",
  deleted: "remove-circle",
  renamed: "swap-horizontal",
  modified: "circle",
  conflict: "warning",
  untracked: "cloud-upload",
  binary: "cube",
};

const STATUS_GLYPHS: Record<string, string> = {
  added: "A",
  deleted: "D",
  renamed: "R",
  copied: "C",
  conflict: "!",
  untracked: "U",
  modified: "M",
  changed: "M",
};

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

export function describeGitDiffFile(
  file: GitDiffFileInfo,
  scope: GitDiffScope,
): GitDiffFilePresentation {
  const { directory, name } = splitGitDiffPath(file.path);
  const oldPath = file.old_path ? splitGitDiffPath(file.old_path) : null;
  const tone: GitDiffStatusTone = file.binary
    ? "binary"
    : STATUS_TONES[file.status] ?? "modified";
  const [additions, deletions] = gitDiffCounts(file, scope);
  const statusLabel = file.status
    ? file.status.charAt(0).toUpperCase() + file.status.slice(1)
    : "Changed";

  return {
    name,
    directory,
    oldName: oldPath?.name ?? null,
    oldDirectory: oldPath?.directory ?? null,
    statusLabel,
    scopeLabel: describeGitDiffScope(file),
    icon: TONE_ICONS[tone],
    glyph: STATUS_GLYPHS[file.status] ?? "M",
    tone,
    additions,
    deletions,
    binary: Boolean(file.binary),
  };
}

/** Scope words worth showing on a row; the default working change stays quiet. */
export function gitDiffRowScopeNote(
  file: GitDiffFileInfo,
  scope: GitDiffScope,
): string | null {
  if (scope !== "all" || file.untracked) return null;
  if (file.staged && file.unstaged) return "Staged + unstaged";
  if (file.staged) return "Staged";
  return null;
}

/** `+12 −3 · 4 files` style summary for a set of changed files. */
export function summarizeGitDiffFiles(
  files: readonly GitDiffFileInfo[],
  scope: GitDiffScope,
): { additions: number; deletions: number; label: string } {
  let additions = 0;
  let deletions = 0;
  for (const file of files) {
    if (file.binary) continue;
    const [added, deleted] = gitDiffCounts(file, scope);
    additions += added;
    deletions += deleted;
  }
  const count = `${files.length} ${files.length === 1 ? "file" : "files"}`;
  return {
    additions,
    deletions,
    label: files.length ? `+${additions} \u2212${deletions} \u00b7 ${count}` : "No changes",
  };
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
