import type { WorkerDirectorySection } from "../../services/workerDirectory";
import type { WorkStatus } from "../ui/workStatus";

/** A directory section as the list draws it: folded sections keep their rows out of `data`. */
export type SessionListSection = Omit<WorkerDirectorySection, "data"> & {
  data: WorkerDirectorySection["data"];
  /** Every Session in the directory, folded or not. */
  count: number;
  collapsed: boolean;
  /** The most urgent state among a folded section's hidden Sessions. */
  mark: WorkStatus | null;
};

/** Folded sections are remembered, but never more than this many. */
export const MAX_REMEMBERED_COLLAPSED_SECTIONS = 200;

/**
 * What a folded directory still has to say: a Session waiting on you, then a
 * failure, then work in flight. Everything else stays quiet, like its rows.
 */
export function sectionAttentionMark(
  workers: readonly { status: string; needs_attention?: boolean }[],
): WorkStatus | null {
  if (workers.some((worker) => worker.needs_attention === true)) return "needs";
  if (workers.some((worker) => worker.status === "failed")) return "failed";
  if (workers.some((worker) => worker.status === "running")) return "running";
  return null;
}

/**
 * Folds are ignored while selecting, so a selected Session is never hidden.
 * Only a list with several directories has headers, so only it can fold.
 */
export function buildSessionListSections(
  sections: readonly WorkerDirectorySection[],
  collapsedKeys: ReadonlySet<string>,
  selectionMode: boolean,
): SessionListSection[] {
  const foldable = sections.length > 1 && !selectionMode;
  return sections.map((section) => {
    const collapsed = foldable && collapsedKeys.has(section.key);
    return {
      ...section,
      data: collapsed ? [] : section.data,
      count: section.data.length,
      collapsed,
      mark: collapsed ? sectionAttentionMark(section.data) : null,
    };
  });
}

export function toggleCollapsedSection(
  keys: ReadonlySet<string>,
  key: string,
): Set<string> {
  const next = new Set(keys);
  if (!next.delete(key)) next.add(key);
  return next;
}

/** Oldest folds fall away first once the cap is reached. */
export function collapsedSectionsToStore(keys: ReadonlySet<string>): string[] {
  return [...keys].slice(-MAX_REMEMBERED_COLLAPSED_SECTIONS);
}
