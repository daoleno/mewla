import { computed, signal } from "nsh";
import { workOfSnapshot } from "../../core/work/brainWork";
import { brainWorkCalendarOrigins, brainWorkSurface } from "../../core/work/surface";
import { WorkItems, type WorkItem } from "../../core/work/workItems";
import { clock } from "../calendar/clock";
import { calendarItems } from "../calendar/state";
import { brain, onTransport } from "../session";

/** Brain's Work on the current computer, as slips grouped by what they ask of you. */
export const workSurface = computed(() => {
  const snapshot = brain() as Record<string, unknown> | null;
  const work = workOfSnapshot(snapshot);
  const c = clock();
  const surface = brainWorkSurface(work.current, brain()?.workers, c.now, work.open);
  return { ...brainWorkCalendarOrigins(surface, calendarItems(), c), objective: work.objective };
});

/** The current computer's Markdown Work files (a Calendar run's Work). */
export const workFiles = signal<WorkItem[]>([]);
export const workFilesModel = signal<WorkItems | null>(null);

/**
 * The Work file a Session writes into, the newest when there are several: what a
 * Session's "Open Brain" opens (`WorkFilePage` with this id).
 */
export function workFileOfSession(sessionId: string): string | undefined {
  return workFiles()
    .filter((item) => item.frontmatter.worker_session === sessionId)
    .sort((a, b) => Date.parse(b.mtime) - Date.parse(a.mtime))[0]?.id;
}

/**
 * The Work file of a Brain Work (SPEC §11 Work detail's "Work file" row), the newest when there are several:
 * a Calendar run's file (the run's Work id, or its run), else the one a Worker of it writes into.
 */
export function workFileOfWork(work: { workId: string; runId?: string; sessionIds: readonly string[] }): WorkItem | undefined {
  return workFiles()
    .filter((item) => item.id === work.workId
      || (Boolean(work.runId) && item.frontmatter.extra?.calendar_run_id === work.runId)
      || (Boolean(item.frontmatter.worker_session) && work.sessionIds.includes(item.frontmatter.worker_session!)))
    .sort((a, b) => Date.parse(b.mtime) - Date.parse(a.mtime))[0];
}

onTransport((transport) => {
  const model = new WorkItems(transport);
  workFilesModel.set(model);
  const stop = model.onChange(() => workFiles.set([...model.items.values()]));
  return () => {
    stop();
    model.close();
    workFilesModel.set(null);
    workFiles.set([]);
  };
});
