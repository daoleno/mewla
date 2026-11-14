import type { ActivityTimelineItem } from "./activityTypes";

/** A file an activity row lists: the path it shows and opens (a rename's new one), and its +/− when the provider counted them. */
export type ActivityFileRow = {
  path: string;
  /** The old path of a renamed file. */
  from?: string;
  added?: number;
  removed?: number;
};

const sum = (a: number | undefined, b: number | undefined) => (a === undefined && b === undefined ? undefined : (a ?? 0) + (b ?? 0));

/**
 * The files a tool row (or a folded run of them) touched, in order, each once (SPEC §1.7 Edited files):
 * a patch's summaries with their +/− counts, where a renamed file shows and opens its new path, then
 * any other file the row names. A file edited by several steps of a run adds up its counts.
 */
export function activityFileRows(item: ActivityTimelineItem): ActivityFileRow[] {
  const rows = new Map<string, ActivityFileRow>();
  const renamed = new Set<string>();
  const visit = (row: ActivityTimelineItem) => {
    for (const file of row.fileSummaries ?? []) {
      const path = file.movePath || file.path;
      if (file.movePath && file.movePath !== file.path) renamed.add(file.path);
      const seen = rows.get(path);
      rows.set(path, seen
        ? { ...seen, added: sum(seen.added, file.added), removed: sum(seen.removed, file.removed) }
        : { path, ...(file.movePath && file.movePath !== file.path ? { from: file.path } : {}), ...(file.added !== undefined ? { added: file.added } : {}), ...(file.removed !== undefined ? { removed: file.removed } : {}) });
    }
    for (const file of row.files ?? []) {
      if (!rows.has(file) && !renamed.has(file)) rows.set(file, { path: file });
    }
  };
  if (item.steps?.length) item.steps.forEach(visit);
  else visit(item);
  return [...rows.values()];
}
