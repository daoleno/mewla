import type { BrainCurrentWork } from "../../store/brain";
import type { ConnectionState } from "../../store/workers";

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
  /** The Work the state is about, when there is one. */
  workTitle?: string;
}

/**
 * Brain's state between turns. A running turn is "working" and belongs to the
 * timeline's Working row, which outranks every state here.
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
  const work = currentWork ?? [];
  const needsInput = work.find((item) => item.status === "needs_input");
  if (needsInput) return { state: "attention", workTitle: needsInput.title };
  const unread = work.find((item) => item.unread_result);
  if (unread) return { state: "delivered", workTitle: unread.title };
  const delegated = work.find(
    (item) =>
      item.attempt_delegated &&
      (item.status === "running" || item.status === "waiting"),
  );
  if (delegated) return { state: "delegating", workTitle: delegated.title };
  return { state: "idle" };
}

/** States the timeline's tail row announces when no turn is running. */
export function brainCatTailLabel(presence: BrainCatPresence): string | null {
  switch (presence.state) {
    case "attention":
      return "Needs you";
    case "delivered":
      return "Brought something back";
    case "delegating":
      return "Waiting on Workers";
    default:
      return null;
  }
}
