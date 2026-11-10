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
