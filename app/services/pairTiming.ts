/**
 * Where the time goes when a computer is paired. Pairing is the first thing a
 * new person does, and a slow one looks like nothing happened, so every hop
 * from the opened link to the first live connection is timed and the slow ones
 * are reported.
 */

export interface PairSegment {
  name: string;
  ms: number;
  /** What happened in that hop, e.g. the error a request ended with. */
  detail?: string;
}

/** A pairing slower than this (the confirm prompt is the person's, not ours) says where it went. */
export const SLOW_PAIRING_MS = 8_000;

// The confirm prompt waits on a person; it is shown but never counted.
const HUMAN_SEGMENTS = new Set(["confirm"]);

export class PairTimeline {
  private readonly started: number;
  private last: number;
  private readonly marks: PairSegment[] = [];

  constructor(private readonly now: () => number = Date.now) {
    this.started = now();
    this.last = this.started;
  }

  /** Closes the hop that ran since the previous mark under `name`. */
  mark(name: string, detail?: string): void {
    const at = this.now();
    this.marks.push(
      detail ? { name, ms: Math.max(0, at - this.last), detail } : { name, ms: Math.max(0, at - this.last) },
    );
    this.last = at;
  }

  segments(): PairSegment[] {
    return [...this.marks];
  }

  /** Elapsed time without the hops that wait on the person. */
  workMs(): number {
    return this.marks
      .filter((segment) => !HUMAN_SEGMENTS.has(segment.name))
      .reduce((sum, segment) => sum + segment.ms, 0);
  }

  slowest(): PairSegment | null {
    let slowest: PairSegment | null = null;
    for (const segment of this.marks) {
      if (HUMAN_SEGMENTS.has(segment.name)) continue;
      if (!slowest || segment.ms > slowest.ms) slowest = segment;
    }
    return slowest;
  }

  summary(): string {
    return this.marks.map((segment) => `${segment.name} ${formatMs(segment.ms)}`).join(" · ");
  }

  ageMs(): number {
    return this.now() - this.started;
  }

  isSlow(): boolean {
    return this.workMs() >= SLOW_PAIRING_MS;
  }
}

export function formatMs(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.round(ms)}ms`;
}

// A pairing whose computer never connected is forgotten, so a reconnect an
// hour later is not reported as a slow pairing.
const PAIRING_FORGOTTEN_MS = 5 * 60_000;

const UNFINISHED_REPORT_MS = 90_000;
let unfinishedTimer: ReturnType<typeof setTimeout> | null = null;

function clearUnfinishedTimer(): void {
  if (unfinishedTimer) clearTimeout(unfinishedTimer);
  unfinishedTimer = null;
}

let active: PairTimeline | null = null;
let activeFor: string | null = null;

/** Begins timing a pairing; a pairing still being timed is dropped. */
export function startPairTimeline(now?: () => number): PairTimeline {
  clearUnfinishedTimer();
  active = new PairTimeline(now);
  activeFor = null;
  return active;
}

/** Names the saved computer the timeline belongs to, so its first connection can close it. */
export function bindPairTimeline(serverId: string): void {
  if (!active) return;
  activeFor = serverId;
  // A computer that never reaches a live connection still leaves its timeline.
  const timeline = active;
  if (unfinishedTimer) clearTimeout(unfinishedTimer);
  unfinishedTimer = setTimeout(() => {
    unfinishedTimer = null;
    if (active !== timeline) return;
    timeline.mark("waiting");
    report(timeline, "unfinished");
  }, UNFINISHED_REPORT_MS);
}

export interface PairTimelineReport {
  /** How it ended: "connected", or the failure ("failed", "cancelled"). */
  outcome: string;
  summary: string;
  segments: PairSegment[];
  workMs: number;
  slowest: PairSegment | null;
  slow: boolean;
  finishedAt: number;
}

type PairTimelineSink = (report: PairTimelineReport) => void;
let sink: PairTimelineSink | null = null;

/** Where finished reports are kept, so a slow pairing can be read afterwards. */
export function setPairTimelineSink(next: PairTimelineSink | null): void {
  sink = next;
}

function report(timeline: PairTimeline, outcome: string): PairTimelineReport {
  const result: PairTimelineReport = {
    outcome,
    summary: timeline.summary(),
    segments: timeline.segments(),
    workMs: timeline.workMs(),
    slowest: timeline.slowest(),
    slow: timeline.isSlow(),
    finishedAt: Date.now(),
  };
  console.log(`[pair-timing] ${outcome} · work ${formatMs(result.workMs)} · ${result.summary}`);
  try {
    sink?.(result);
  } catch {
    // Keeping the report is best effort.
  }
  return result;
}

/**
 * The imported computer reached its first live state (or gave up). Returns the
 * finished report once, and nothing for any other computer.
 */
export function finishPairTimeline(
  serverId: string,
  mark = "connected",
): PairTimelineReport | null {
  if (!active || activeFor !== serverId) return null;
  if (active.ageMs() > PAIRING_FORGOTTEN_MS) {
    active = null;
    activeFor = null;
    return null;
  }
  const timeline = active;
  active = null;
  activeFor = null;
  clearUnfinishedTimer();
  timeline.mark(mark);
  return report(timeline, "connected");
}

/** A pairing that ended without a computer to connect to. */
export function abandonPairTimeline(reason: string): PairTimelineReport | null {
  if (!active) return null;
  const timeline = active;
  active = null;
  activeFor = null;
  clearUnfinishedTimer();
  timeline.mark(reason);
  return report(timeline, reason);
}

/** Marks the running pairing, if any. */
export function markPairTimeline(name: string, detail?: string): void {
  active?.mark(name, detail);
}

export function slowPairingToast(finished: PairTimelineReport): {
  title: string;
  detail: string;
  tone: "info";
} {
  const slowest = finished.slowest;
  return {
    title: `Pairing took ${formatMs(finished.workMs)}`,
    detail: slowest ? `Slowest: ${slowest.name} ${formatMs(slowest.ms)}. ${finished.summary}` : finished.summary,
    tone: "info",
  };
}
