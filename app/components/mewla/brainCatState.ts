import type { BrainCurrentWork } from "../../store/brain";
import type { ConnectionState } from "../../store/workers";
import { brainWorkSurface } from "../brain/brainWorkSurface";

/**
 * What the one cat is doing. It mirrors Brain's real state and nothing else:
 * no idle tricks, no decoration.
 *
 * - homeless: no computer paired, so the seal is an empty bed
 * - offline: the computer is unreachable; asleep in a greyed seal
 * - waking: connecting or loading; one eye open in the seal
 * - idle: connected with nothing to do; curled up asleep in the seal
 * - working: Brain's turn is running; out of the seal and on its feet
 * - delegating: Workers hold delegated Work; sitting, watching them
 * - attention: Work needs your input; ears up, looking at you
 * - delivered: an unread result is waiting; it brought you something
 */
export type BrainCatState =
  | "homeless"
  | "offline"
  | "waking"
  | "idle"
  | "working"
  | "delegating"
  | "attention"
  | "delivered";

export interface BrainCatPresence {
  state: BrainCatState;
  /** How many Work items the state is about (needs you, back, delegated). */
  count?: number;
  /** Work that needs you, newest-first by the daemon's order; the cat perches on one of its slips. */
  workIds?: readonly string[];
}

/**
 * Brain's state between turns. A running turn is "working" and belongs to the
 * timeline's Working row, which outranks every state here. It reads the same
 * grouping as the Work surface, so the cat and the summary line agree.
 */
export function resolveBrainCatPresence({
  hasServer,
  connection,
  hydrated,
  currentWork,
}: {
  hasServer: boolean;
  connection: ConnectionState;
  hydrated: boolean;
  currentWork?: readonly BrainCurrentWork[];
}): BrainCatPresence {
  if (!hasServer) return { state: "homeless" };
  if (connection === "offline") return { state: "offline" };
  if (connection !== "connected" || !hydrated) return { state: "waking" };
  const { slips, counts } = brainWorkSurface(currentWork, undefined);
  if (counts.needs) {
    return {
      state: "attention",
      count: counts.needs,
      workIds: slips.filter((slip) => slip.group === "needs").map((slip) => slip.workId),
    };
  }
  if (counts.back) return { state: "delivered", count: counts.back };
  const delegated = (currentWork ?? []).filter(
    (item) =>
      item.attempt_delegated &&
      (item.status === "running" || item.status === "waiting"),
  ).length;
  if (delegated) return { state: "delegating", count: delegated };
  return { state: "idle" };
}

/** States the timeline's tail row announces when no turn is running. */
export function brainCatTailLabel(presence: BrainCatPresence): string | null {
  const count = presence.count ?? 1;
  switch (presence.state) {
    case "attention":
      return count > 1 ? `${count} need you` : "Needs you";
    case "delivered":
      return count > 1 ? `Brought ${count} things back` : "Brought something back";
    case "delegating":
      return count > 1 ? `Waiting on ${count} Workers` : "Waiting on a Worker";
    default:
      return null;
  }
}
