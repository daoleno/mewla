import type {
  GitDiffFileContentPayload,
  GitDiffFileInfo,
  GitDiffPatchPayload,
} from "./gitDiff";
import type { GitReviewGroup } from "./gitReviewModel";

/** One patch the reader shows (NSH `DiffView`'s `patch`), labelled when a file has several. */
export interface GitDiffSection {
  /** Unique within one file's review. */
  id: string;
  /** Shown before the patch when one file has several sections. */
  label?: string;
  patch: string;
  /** Both full sides can be asked for, so collapsed context can expand. */
  expandable: boolean;
}

/**
 * A comparison larger than this is not shown: the reader would hold and parse
 * the whole patch at once.
 */
export const GIT_DIFF_PATCH_LIMIT_BYTES = 5 * 1024 * 1024;

export type GitDiffReviewPlan =
  | { kind: "sections"; sections: GitDiffSection[]; bytes: number }
  | { kind: "empty" }
  | { kind: "too-large"; bytes: number };

/** The daemon sections of one group: Changes is the unstaged and untracked ones. */
export function gitDiffSectionsForGroup(payload: GitDiffPatchPayload, group: GitReviewGroup) {
  return payload.sections.filter((section) => (group === "staged" ? section.scope === "staged" : section.scope !== "staged"));
}

/**
 * Collapsed context can expand only when the full old and new sides are what
 * the daemon returns: HEAD and the working tree. A file with both staged and
 * unstaged edits has an index in between, and an untracked or binary file has
 * nothing to expand, so those keep their hunks as they are.
 */
export function canExpandGitDiffFile(file: GitDiffFileInfo): boolean {
  if (file.binary || file.untracked) return false;
  if (file.staged === file.unstaged) return false;
  return file.status === "modified" || file.status === "changed" || file.status === "renamed";
}

export function planGitDiffReview(
  payload: GitDiffPatchPayload,
  file: GitDiffFileInfo,
  group: GitReviewGroup,
): GitDiffReviewPlan {
  const selected = gitDiffSectionsForGroup(payload, group).filter((section) => section.patch.trim() !== "");
  if (!selected.length) return { kind: "empty" };
  const bytes = selected.reduce((total, section) => total + section.patch.length, 0);
  if (bytes > GIT_DIFF_PATCH_LIMIT_BYTES) return { kind: "too-large", bytes };
  const expandable = canExpandGitDiffFile(file);
  return {
    kind: "sections",
    bytes,
    sections: selected.map((section, index) => ({
      id: `${index}-${section.scope}`,
      // The path already titles the sheet; a label only tells sections of one file apart.
      label: selected.length > 1 ? section.title : undefined,
      patch: `${section.patch}\n`,
      expandable,
    })),
  };
}

/** What Copy diff puts on the clipboard: every patch of the group, as git printed them. */
export function gitDiffPatchText(payload: GitDiffPatchPayload, group: GitReviewGroup): string {
  return gitDiffSectionsForGroup(payload, group)
    .filter((section) => section.patch.trim() !== "")
    .map((section) => `${section.patch}\n`)
    .join("");
}

export interface GitDiffExpansionSides {
  oldContents: string;
  newContents: string;
  oldName?: string;
}

/** The full sides for hunk expansion, or null when either side cannot be shown whole. */
export function gitDiffExpansionSides(
  payload: GitDiffFileContentPayload,
  file: GitDiffFileInfo,
): GitDiffExpansionSides | null {
  const { base, current } = payload;
  for (const side of [base, current]) {
    if (!side.exists || side.binary || side.truncated) return null;
  }
  return {
    oldContents: base.content ?? "",
    newContents: current.content ?? "",
    oldName: file.old_path,
  };
}

export function formatGitDiffBytes(bytes: number): string {
  return bytes >= 1024 * 1024
    ? `${(bytes / (1024 * 1024)).toFixed(1)} MB`
    : `${Math.max(1, Math.round(bytes / 1024))} KB`;
}
