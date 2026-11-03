import type { BrainExecutorRef, BrainExecutorState } from "../../store/brain";

export interface ExecutorAccountRow {
  id: string;
  name: string;
  /** The account's email, else Signed in or Not signed in. */
  value: string;
}

/** One row per installed agent on the current computer: who it is signed in as. */
export function executorAccountRows(
  executors: BrainExecutorRef[],
  states: Record<string, BrainExecutorState> | undefined,
): ExecutorAccountRow[] {
  const rows: ExecutorAccountRow[] = [];
  for (const executor of executors) {
    const state = states?.[executor.id];
    if (!state?.installed) continue;
    rows.push({
      id: executor.id,
      name: executor.name || executor.id,
      value: state.account || (state.signed_in ? "Signed in" : "Not signed in"),
    });
  }
  return rows;
}
