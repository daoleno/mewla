import { brainWorkSurface } from "../work/surface";
import type { BrainCurrentWork } from "../work/types";

/** A Session of the current computer waits on you (the dot on Sessions). */
export function sessionsNeedYou(workers: readonly { needs_attention?: boolean }[]): boolean {
  return workers.some((worker) => worker.needs_attention === true);
}

/** Brain's current Work waits on you: a question, or a result nobody decided on (the dot on Brain). */
export function brainNeedsYou(currentWork: readonly BrainCurrentWork[] | undefined, now: number = Date.now()): boolean {
  return brainWorkSurface(currentWork, undefined, now).counts.needs > 0;
}

/**
 * A tab's title with its dot. NSH's TabBar has no badge (an upstream gap), so a
 * place that waits on you says so in its title; the selected tab needs none.
 */
export function tabTitle(title: string, needsYou: boolean, selected: boolean): string {
  return needsYou && !selected ? `${title} •` : title;
}
