import type { BrainSnapshot } from "../store/brain";

/**
 * Where a Brain host switch stands, read from the snapshots the daemon already
 * pushes. The daemon answers `brain_set_executor` as soon as the choice is kept;
 * opening the new agent takes as long as the agent takes, so "done" is the new
 * host showing up in the snapshot and "failed" is a host problem for it, never
 * a timer on the request.
 */
export type BrainHostSwitchProgress =
  | { state: "pending" }
  | { state: "ready" }
  | { state: "failed"; detail: string };

/**
 * How long the sheet keeps showing a switch it has not seen finish. Past it the
 * switch is no longer shown as in progress, and nothing is reported as failed:
 * the daemon keeps starting the host, and says so in a host problem if it cannot.
 */
export const BRAIN_HOST_SWITCH_PATIENCE_MS = 90_000;

export function brainHostSwitchProgress(
  brain:
    | Pick<BrainSnapshot, "host_executor" | "host_worker" | "host_problem">
    | null
    | undefined,
  targetExecutorId: string,
): BrainHostSwitchProgress {
  const problem = brain?.host_problem;
  if (problem && problem.executor === targetExecutorId) {
    return { state: "failed", detail: problem.detail };
  }
  const executor = brain?.host_executor;
  const worker = brain?.host_worker;
  // The host worker names its provider only once the daemon observes the
  // session, so a worker of the new executor's provider is the new host (or
  // the same provider's host, which keeps running across the switch).
  if (
    executor?.id === targetExecutorId &&
    executor.provider &&
    worker?.provider === executor.provider
  ) {
    return { state: "ready" };
  }
  return { state: "pending" };
}
