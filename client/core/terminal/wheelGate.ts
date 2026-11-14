/**
 * A drag on a program's own screen (the alternate screen) is remote control: each line of finger travel
 * becomes a wheel press or cursor key the program answers with a redraw. Done right that is
 *
 * - never more in flight than the program has answered: one write, then the travel made meanwhile
 *   waits and goes as one write when the answer (the next output) comes, or after ANSWER_MS without one;
 * - at most one write a frame;
 * - nothing after the finger lifts but the travel it made (a fling is the view's, on the device's clock,
 *   and a touch stops it: `stop`), and a person who outruns a slow link loses travel past MAX_OWED rather
 *   than watching the screen move on long after;
 * - as many presses as move the screen as far as the finger went: a planner knows what one press does.
 *   Most programs move a line a press (or a key); Claude Code accelerates presses that come close together
 *   (ClaudeWheel), so it gets fewer of them.
 */
export const ANSWER_MS = 250;
export const FRAME_MS = 16;
/** Travel a planner that cannot move now (Claude after a fast run) tries again after: past its 40 ms window. */
export const RETRY_MS = 45;
/** The most travel that waits; more is dropped. */
export const MAX_OWED = 40;
/** The most presses one write carries. */
export const MAX_PRESSES = 30;

/** What `presses` presses do now: `lines` the screen moves. `owed` > 0 is back in time; the plan has its sign. */
export interface WheelPlanner {
  plan(owed: number, now: number): { presses: number; lines: number };
  reset(): void;
}

/** A line a press: `less`, Codex's keys, and any program not known to do otherwise. */
export class LinePlanner implements WheelPlanner {
  plan(owed: number): { presses: number; lines: number } {
    const n = Math.min(MAX_PRESSES, Math.trunc(Math.abs(owed)));
    return { presses: Math.sign(owed) * n, lines: Math.sign(owed) * n };
  }
  reset(): void {}
}

/**
 * Claude Code's wheel (2.1.x, its native-terminal path; the fullscreen view's scroll handler), so the
 * phone can send the presses that move Claude as far as the finger went:
 *
 * - presses closer than 40 ms raise a multiplier by 0.3 a press, up to 6 (10 presses in one write move
 *   19 rows, measured); one 40 ms or more after the last starts again at one row;
 * - the first press against the last direction moves nothing (a wheel's bounce), and a press back in the
 *   old direction within 200 ms of it turns on "wheel mode", which accelerates up to 15 rows a press
 *   until 1.5 s of quiet or five presses under 5 ms apart.
 *
 * The constants are Claude's. A model that drifts from Claude (another version, a wheel at the desk)
 * moves the screen a few lines more or less, never in the wrong direction.
 */
export class ClaudeWheel implements WheelPlanner {
  private time = 0;
  private mult = 1;
  private dir = 0;
  private pendingFlip = false;
  private wheelMode = false;
  private burstCount = 0;

  /** Rows one press in direction `dir` (+1 or -1) moves at `now`, as Claude counts them. */
  press(dir: number, now: number): number {
    const base = 1;
    if (this.wheelMode && now - this.time > 1500) {
      this.wheelMode = false;
      this.burstCount = 0;
      this.mult = base;
    }
    if (this.pendingFlip) {
      this.pendingFlip = false;
      if (dir !== this.dir || now - this.time > 200) {
        this.dir = dir;
        this.time = now;
        this.mult = base;
        return 1;
      }
      this.wheelMode = true;
    }
    const gap = now - this.time;
    if (dir !== this.dir && this.dir !== 0) {
      this.pendingFlip = true;
      this.time = now;
      return 0;
    }
    this.dir = dir;
    this.time = now;
    if (this.wheelMode) {
      if (gap < 5) {
        if (++this.burstCount >= 5) {
          this.wheelMode = false;
          this.burstCount = 0;
          this.mult = base;
        } else return 1;
      } else this.burstCount = 0;
    }
    if (this.wheelMode) {
      const decay = Math.pow(0.5, gap / 150);
      const next = 1 + (this.mult - 1) * decay + 15 * decay;
      this.mult = Math.min(15, next, this.mult + 3);
      return Math.max(1, Math.floor(this.mult));
    }
    if (gap > 40) this.mult = base;
    else this.mult = Math.min(6, this.mult + 0.3);
    return Math.max(1, Math.floor(this.mult));
  }

  private clone(): ClaudeWheel {
    return Object.assign(new ClaudeWheel(), this);
  }

  /** The presses in one write (all at `now`) that move at most `owed` rows: none when even one would overshoot. */
  plan(owed: number, now: number): { presses: number; lines: number } {
    const want = Math.trunc(Math.abs(owed));
    const dir = Math.sign(owed);
    if (!want) return { presses: 0, lines: 0 };
    const trial = this.clone();
    let presses = 0;
    let moved = 0;
    while (presses < MAX_PRESSES && moved < want) {
      const probe = trial.clone();
      const rows = probe.press(dir, now);
      if (moved + rows > want) break;
      trial.press(dir, now);
      presses++;
      moved += rows;
    }
    // A run that moves nothing (a bounce press alone) is not sent: it would move nothing.
    if (!moved) return { presses: 0, lines: 0 };
    for (let i = 0; i < presses; i++) this.press(dir, now);
    return { presses: dir * presses, lines: dir * moved };
  }

  reset(): void {
    Object.assign(this, new ClaudeWheel());
  }
}

export type GateClock = {
  now(): number;
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(handle: unknown): void;
};

const realClock: GateClock = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
};

/** One write in flight, one a frame; `send(presses)` writes them (> 0 back in time). */
export class WheelGate {
  private owed = 0;
  private inFlight = false;
  private lastSend = -Infinity;
  private answerTimer: unknown = null;
  private pumpTimer: unknown = null;

  constructor(
    private readonly send: (presses: number) => void,
    private planner: WheelPlanner = new LinePlanner(),
    private readonly clock: GateClock = realClock,
  ) {}

  /** Another planner (the program changed); the travel waiting stays. */
  use(planner: WheelPlanner): void {
    if (planner !== this.planner) this.planner = planner;
  }

  /** `lines` more of finger travel, > 0 back in time. */
  add(lines: number): void {
    if (!Number.isFinite(lines) || !lines) return;
    this.owed = Math.max(-MAX_OWED, Math.min(MAX_OWED, this.owed + lines));
    this.pump();
  }

  /** The program answered (output came): the next write may go. */
  answered(): void {
    if (!this.inFlight) return;
    this.inFlight = false;
    this.clock.clearTimeout(this.answerTimer);
    this.answerTimer = null;
    this.pump();
  }

  /** A finger came down: what waits is dropped (a fling's tail, a drag that ended). */
  stop(): void {
    this.owed = 0;
    this.clock.clearTimeout(this.pumpTimer);
    this.pumpTimer = null;
  }

  /** A new attachment or none: nothing in flight, nothing waiting. */
  reset(): void {
    this.stop();
    this.inFlight = false;
    this.clock.clearTimeout(this.answerTimer);
    this.answerTimer = null;
    this.lastSend = -Infinity;
    this.planner.reset();
  }

  /** Travel that waits, for tests and the view. */
  get waiting(): number {
    return this.owed;
  }

  get busy(): boolean {
    return this.inFlight;
  }

  private later(ms: number): void {
    if (this.pumpTimer !== null) return;
    this.pumpTimer = this.clock.setTimeout(() => {
      this.pumpTimer = null;
      this.pump();
    }, ms);
  }

  private pump(): void {
    if (this.inFlight || !Math.trunc(this.owed)) return;
    const now = this.clock.now();
    const wait = this.lastSend + FRAME_MS - now;
    if (wait > 0) return this.later(wait);
    const plan = this.planner.plan(this.owed, now);
    if (!plan.presses) return this.later(RETRY_MS);
    this.owed -= plan.lines;
    this.inFlight = true;
    this.lastSend = now;
    this.answerTimer = this.clock.setTimeout(() => this.answered(), ANSWER_MS);
    this.send(plan.presses);
  }
}
