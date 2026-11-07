/**
 * Dev-only stopwatch for "tap the cat's Work hint → the Work list is drawn".
 * The stages are the JS side of one open: the finger goes down, the press
 * fires, the sheet commits, and the next frame is requested. In a development
 * build the breakdown lands in Metro's log; on web it is also kept on
 * `globalThis.__workOpenTimings` so a harness can read it. Release builds
 * (`__DEV__` false) record nothing.
 */
export type WorkOpenStage = "pressIn" | "press" | "commit" | "frame";

export interface WorkOpenTiming {
  /** Finger down to the press firing (a hold or a slow release). */
  pressInToPress: number;
  /** The press firing to the sheet's commit: the JS work of opening. */
  pressToCommit: number;
  /** The commit to the next frame: layout and paint. */
  commitToFrame: number;
  total: number;
}

const stamps: Partial<Record<WorkOpenStage, number>> = {};

function now(): number {
  return typeof performance !== "undefined" ? performance.now() : Date.now();
}

function dev(): boolean {
  return typeof __DEV__ !== "undefined" && __DEV__;
}

/** Pure: the breakdown of one open from its stamps, or null while incomplete. */
export function workOpenBreakdown(marks: Partial<Record<WorkOpenStage, number>>): WorkOpenTiming | null {
  const { press, commit, frame } = marks;
  if (press === undefined || commit === undefined || frame === undefined) return null;
  const start = marks.pressIn ?? press;
  return {
    pressInToPress: round(press - start),
    pressToCommit: round(commit - press),
    commitToFrame: round(frame - commit),
    total: round(frame - start),
  };
}

function round(value: number): number {
  return Math.round(value * 10) / 10;
}

export function markWorkOpen(stage: WorkOpenStage): void {
  if (!dev()) return;
  if (stage === "pressIn") {
    for (const key of Object.keys(stamps)) delete stamps[key as WorkOpenStage];
  }
  stamps[stage] = now();
  if (stage !== "frame") return;
  const timing = workOpenBreakdown(stamps);
  // One open per stopwatch: the next press starts clean.
  for (const key of Object.keys(stamps)) delete stamps[key as WorkOpenStage];
  if (!timing) return;
  const shared = globalThis as { __workOpenTimings?: WorkOpenTiming[] };
  (shared.__workOpenTimings ??= []).push(timing);
  console.log(
    `[work-open] pressIn→press ${timing.pressInToPress} ms · press→commit ${timing.pressToCommit} ms · commit→frame ${timing.commitToFrame} ms · total ${timing.total} ms`,
  );
}
