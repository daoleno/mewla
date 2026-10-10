import type { BrainCurrentWork } from "../../store/brain";
import type { ConnectionState } from "../../store/workers";
import { brainWorkSurface, type BrainWorkGroup } from "../brain/brainWorkSurface";

/**
 * What the one cat is doing. It mirrors Brain's real state and nothing else:
 * no idle tricks, no decoration.
 *
 * - homeless: no computer paired, so the seal is an empty bed
 * - offline: the computer is unreachable; asleep in a greyed seal
 * - waking: connecting or loading; one eye open in the seal
 * - idle: connected with nothing to do; curled up asleep in the seal
 * - working: Brain's turn is running; out of the seal and on its feet
 * - delegating: Workers are running (or waiting on) delegated Work; sitting,
 *   watching them
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
  /** The cat is elsewhere on screen (the Work column's perch): no tail row. */
  away?: boolean;
  /** Delegating with nothing running: every delegated Work is waiting. */
  waiting?: boolean;
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
  now = Date.now(),
}: {
  hasServer: boolean;
  connection: ConnectionState;
  hydrated: boolean;
  currentWork?: readonly BrainCurrentWork[];
  now?: number;
}): BrainCatPresence {
  if (!hasServer) return { state: "homeless" };
  if (connection === "offline") return { state: "offline" };
  if (connection !== "connected" || !hydrated) return { state: "waking" };
  const { slips, counts } = brainWorkSurface(currentWork, undefined, now);
  if (counts.needs) {
    return {
      state: "attention",
      count: counts.needs,
      workIds: slips.filter((slip) => slip.group === "needs").map((slip) => slip.workId),
    };
  }
  if (counts.back) return { state: "delivered", count: counts.back };
  // Delegated Work in the Work list's Running group. The tail row is where
  // that count shows; the Work column carries no count of its own.
  const delegated = new Set(
    (currentWork ?? [])
      .filter((item) => item.attempt_delegated && (item.status === "running" || item.status === "waiting"))
      .map((item) => item.work_id),
  );
  const running = slips.filter((slip) => slip.group === "running" && delegated.has(slip.workId)).length;
  if (running) return { state: "delegating", count: running };
  if (delegated.size) return { state: "delegating", count: delegated.size, waiting: true };
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
      if (presence.waiting) return count > 1 ? `Waiting on ${count} Workers` : "Waiting on a Worker";
      return count > 1 ? `${count} Workers running` : "1 Worker running";
    case "idle":
      return "All quiet";
    // The chat stays readable while the link is down; the cat says so.
    case "offline":
      return "Can't reach your computer";
    case "waking":
      return "Waking up";
    default:
      return null;
  }
}

/** The Work list group the tail row counts, if it counts one. */
export function presenceWorkGroup(presence: BrainCatPresence): BrainWorkGroup | null {
  switch (presence.state) {
    case "attention":
      return "needs";
    case "delivered":
      return "back";
    case "delegating":
      return presence.waiting ? "waiting" : "running";
    default:
      return null;
  }
}

/** What tapping the cat does: it answers for Brain, in its current state. */
export type BrainCatTap =
  | { kind: "say"; text: string }
  | { kind: "open-work"; workId: string }
  | { kind: "show-turn"; text: string }
  | { kind: "retry" }
  | { kind: "pair" };

/**
 * The cat's answer to a tap. Offline retries the connection; needs-you
 * jumps to the first slip that needs you; a running turn says what Brain is
 * doing; otherwise a one-line status of Brain's Work.
 */
export function brainCatTap({
  presence,
  turnRunning,
  turnLabel,
  counts,
}: {
  presence: BrainCatPresence;
  turnRunning?: boolean;
  /** The Working row's text, e.g. "Running a command". */
  turnLabel?: string;
  counts: { needs: number; running: number; back: number; waiting: number };
}): BrainCatTap {
  if (presence.state === "homeless") return { kind: "pair" };
  if (presence.state === "offline") return { kind: "retry" };
  if (presence.state === "waking") return { kind: "say", text: "Still waking up. Connecting to your computer…" };
  if (turnRunning || presence.state === "working") {
    return {
      kind: "show-turn",
      text: turnLabel && turnLabel !== "Working" ? `Right now: ${turnLabel}` : "Thinking it through…",
    };
  }
  const first = presence.workIds?.[0];
  if (presence.state === "attention" && first) return { kind: "open-work", workId: first };
  return { kind: "say", text: brainCatStatusLine(counts) };
}

/** "All quiet. 2 running, nothing needs you." */
export function brainCatStatusLine(counts: { needs: number; running: number; back: number; waiting: number }): string {
  const parts: string[] = [];
  if (counts.running) parts.push(`${counts.running} running`);
  if (counts.back) parts.push(`${counts.back} back`);
  if (counts.waiting) parts.push(`${counts.waiting} waiting`);
  const needs = counts.needs
    ? `${counts.needs} ${counts.needs === 1 ? "needs" : "need"} you`
    : "nothing needs you";
  if (!parts.length && !counts.needs) return "All quiet. Nothing out, nothing needs you.";
  return `${counts.needs ? "" : "All quiet. "}${[...parts, needs].join(", ")}.`.replace(/^(\w)/, (c) => c.toUpperCase());
}
