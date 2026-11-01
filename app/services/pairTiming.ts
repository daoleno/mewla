/**
 * Where the time goes when a computer is paired. Pairing is the first thing a
 * new person does, and a slow one looks like nothing happened, so every hop
 * from the opened link to the first live connection is timed and the slow ones
 * are reported.
 */

export interface PairSegment {
  name: string;
  ms: number;
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
  mark(name: string): void {
    const at = this.now();
    this.marks.push({ name, ms: Math.max(0, at - this.last) });
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

let active: PairTimeline | null = null;
let activeFor: string | null = null;

/** Begins timing a pairing; a pairing still being timed is dropped. */
export function startPairTimeline(now?: () => number): PairTimeline {
  active = new PairTimeline(now);
  activeFor = null;
  return active;
}

/** Names the saved computer the timeline belongs to, so its first connection can close it. */
export function bindPairTimeline(serverId: string): void {
  if (active) activeFor = serverId;
}

export interface PairTimelineReport {
  summary: string;
  workMs: number;
  slowest: PairSegment | null;
  slow: boolean;
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
  timeline.mark(mark);
  const report: PairTimelineReport = {
    summary: timeline.summary(),
    workMs: timeline.workMs(),
    slowest: timeline.slowest(),
    slow: timeline.isSlow(),
  };
  console.log(`[pair-timing] total ${formatMs(report.workMs)} · ${report.summary}`);
  return report;
}

/** A pairing that ended without a computer to connect to. */
export function abandonPairTimeline(reason: string): PairTimelineReport | null {
  if (!active) return null;
  const timeline = active;
  active = null;
  activeFor = null;
  timeline.mark(reason);
  const report: PairTimelineReport = {
    summary: timeline.summary(),
    workMs: timeline.workMs(),
    slowest: timeline.slowest(),
    slow: timeline.isSlow(),
  };
  console.log(`[pair-timing] ${reason} after ${formatMs(report.workMs)} · ${report.summary}`);
  return report;
}

/** Marks the running pairing, if any. */
export function markPairTimeline(name: string): void {
  active?.mark(name);
}

export function slowPairingToast(report: PairTimelineReport): {
  title: string;
  detail: string;
  tone: "info";
} {
  const slowest = report.slowest;
  return {
    title: `Pairing took ${formatMs(report.workMs)}`,
    detail: slowest ? `Slowest: ${slowest.name} ${formatMs(slowest.ms)}. ${report.summary}` : report.summary,
    tone: "info",
  };
}
