import { cleanup, effect, signal, untracked } from "nsh";
import type { BrainExecutorRef, BrainSnapshot } from "../../core/brain";
import { BRAIN_HOST_SWITCH_PATIENCE_MS, brainHostSwitchProgress, setBrainExecutor } from "../../core/brainMenu";
import type { Daemon } from "../../core/more/daemon";

/**
 * Switching the agent Brain runs on, one switch at a time, shared by the "Brain
 * host" page and the can't-start notice (Use {suggested}). A choice is in progress
 * until a snapshot shows the new host ("switched"), or fails with the computer's
 * words: its refusal of the request, or the host problem it then reports for that
 * agent. Past the patience nothing is called failed: the computer keeps starting it.
 */
export type BrainHostSwitch = {
  /** The executor being switched to, or null. */
  switching: () => string | null;
  /** Why the last switch did not happen, in the computer's words. */
  error: () => string | null;
  /** Bumped each time a switch ends with the new host up. */
  switched: () => number;
  switchTo: (executor: BrainExecutorRef) => Promise<void>;
  clearError: () => void;
};

/**
 * Make one inside a component (its effects end with it), for the current computer:
 * `snapshot` is its `brain_snapshot.brain`, `transport` its connection, `serverId`
 * which computer it is (a switch of computer forgets a switch in progress).
 */
export function createBrainHostSwitch(source: {
  snapshot: () => BrainSnapshot | null;
  transport: () => Daemon | null;
  serverId: () => string;
  /** The new host is up. */
  onSwitched?: () => void;
}): BrainHostSwitch {
  const switching = signal<string | null>(null);
  const accepted = signal(false);
  const error = signal<string | null>(null);
  const switched = signal(0);
  const reset = () => {
    switching.set(null);
    accepted.set(false);
  };

  effect(() => {
    source.serverId();
    untracked(() => {
      reset();
      error.set(null);
    });
  });

  effect(() => {
    const target = switching();
    if (!target || !accepted()) return;
    const progress = brainHostSwitchProgress(source.snapshot(), target);
    if (progress.state === "pending") return;
    untracked(() => {
      reset();
      if (progress.state === "failed") return error.set(progress.detail);
      switched.set(switched() + 1);
      source.onSwitched?.();
    });
  });

  effect(() => {
    if (!switching() || !accepted()) return;
    const timer = setTimeout(() => untracked(reset), BRAIN_HOST_SWITCH_PATIENCE_MS);
    cleanup(() => clearTimeout(timer));
  });

  return {
    switching: () => switching(),
    error: () => error(),
    switched: () => switched(),
    clearError: () => error.set(null),
    async switchTo(executor) {
      const transport = untracked(source.transport);
      if (!transport || untracked(switching)) return;
      switching.set(executor.id);
      error.set(null);
      try {
        await setBrainExecutor(transport, executor.id);
        // Kept, not finished: the snapshots say when the new host is up.
        accepted.set(true);
      } catch (failure) {
        reset();
        error.set(failure instanceof Error && failure.message ? failure.message : "Failed to switch executor.");
      }
    },
  };
}
