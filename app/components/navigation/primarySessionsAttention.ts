import type { BrainCurrentWork } from "../../store/brain";
import { brainWorkSurface } from "../brain/brainWorkSurface";

/**
 * True when a Session on the current server is waiting on you. It drives the
 * seal dot on "Sessions", one of the three places vermilion is allowed.
 */
export function sessionsNeedYou(
  workers: readonly { serverId: string; needs_attention?: boolean }[],
  currentServerId: string | null | undefined,
): boolean {
  if (!currentServerId) return false;
  return workers.some(
    (worker) => worker.serverId === currentServerId && worker.needs_attention === true,
  );
}

/**
 * True when Brain's current Work waits on you (a question, or a result
 * nobody decided on). It drives the seal dot on "Brain", read from the same
 * grouping as the Work list, so the dot clears when the Work is handled.
 */
export function brainNeedsYou(
  currentWork: readonly BrainCurrentWork[] | undefined,
  now: number = Date.now(),
): boolean {
  return brainWorkSurface(currentWork, undefined, now).counts.needs > 0;
}
