import { computed } from "nsh";
import { brainNeedsYou, sessionsNeedYou } from "../../core/sessions/attention";
import type { BrainCurrentWork } from "../../core/work/types";
import { brain, workers } from "../session";

/** Brain's Work or a Session of the current computer waits on you: the tab's (and sidebar row's) dot. */
export const brainDot = computed(() => brainNeedsYou(brain()?.current_work as BrainCurrentWork[] | undefined));
export const sessionsDot = computed(() => sessionsNeedYou(workers()));

/** The dot of a tab or sidebar place, by its key. */
export function needsYou(key: string): boolean {
  return key === "brain" ? brainDot() : key === "sessions" ? sessionsDot() : false;
}
