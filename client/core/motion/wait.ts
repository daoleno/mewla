import { motion } from "./tokens";
import type { ConnectionState } from "../transport";

/**
 * One place waiting for its data, by SPEC §16.2: nothing is drawn under `wait.quiet`; then a placeholder
 * at the final size, held at least `wait.hold` once drawn; the slow line from `wait.slow`; with the link up
 * and no answer by `wait.fail` (or the place's own bound), the failed state with Try again. A read the link
 * interrupts is not a failure: it waits for the link, shows the computer away at `link.away`, and reads again
 * by itself when the link is back. "Empty" follows only a read that succeeded and returned nothing. Last known
 * content beats a placeholder: a place that shows it keeps it through a refresh, a failure and a link away
 * (dimmed). Port of the reference page's wait engine (site/v/motion/motion.js, `Place` and `Link`).
 *
 * Plain logic on an injected clock, so every timing is tested without a screen (motion/wait.test.ts).
 */

/** What the place's one cell shows. `none`: nothing new (under `wait.quiet`, or nothing yet). */
export type WaitView = "none" | "placeholder" | "content" | "empty" | "failed" | "away";

/** The link to the current computer as the person is told it: up, connecting (under `link.away`), away. */
export type LinkState = "up" | "connecting" | "away";

/** A state forced in dev builds, to feel the rare ones (SPEC 16.4's columns): results held 5 s, never, an error, the link away. */
export type WaitForce = "slow" | "none" | "error" | "offline";

export interface WaitState {
  view: WaitView;
  /** The slow line along the top of what is waiting. */
  slow: boolean;
  /** Last known content while the computer is away. */
  dim: boolean;
  /** A read is in flight (or waiting for the link). */
  reading: boolean;
}

export interface Clock {
  now(): number;
  later(ms: number, run: () => void): unknown;
  cancel(handle: unknown): void;
}

export const systemClock: Clock = {
  now: () => Date.now(),
  later: (ms, run) => setTimeout(run, ms),
  cancel: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
};

export interface WaitOptions {
  /** Reads again: on Try again, the link coming back, a forced state (`start()` alone only begins the wait for a read the place started). */
  read?: () => void;
  /** The place's honest bound for "no answer" (approve 8 s, git 20–90 s); `wait.fail` by default. */
  failMs?: number;
  link?: LinkState;
  clock?: Clock;
  onChange?: (state: WaitState) => void;
}

/** How long a forced "slow" read takes, and how soon a forced error comes (the reference page's 5 s and 0.9 s). */
export const FORCED = { slowMs: 5_000, errorMs: 900 } as const;

export class Wait {
  private state: WaitState = { view: "none", slow: false, dim: false, reading: false };
  private readonly clock: Clock;
  private readonly failMs: number;
  private timers: unknown[] = [];
  private gen = 0;
  private startedAt = 0;
  private shownAt = 0;
  private done = true;
  private waitingForLink = false;
  private linkState: LinkState;
  private force: WaitForce | null = null;
  private forceLinkTimer: unknown = null;
  private forcedLink: LinkState | null = null;
  private held: { kind: "content" | "empty" | "failed" } | null = null;

  constructor(private readonly options: WaitOptions = {}) {
    this.clock = options.clock ?? systemClock;
    this.failMs = options.failMs ?? motion.wait.fail;
    this.linkState = options.link ?? "up";
  }

  get current(): WaitState {
    return this.state;
  }

  /** Whether the place has shown what it read (content or a successful empty): last known first. */
  get known(): boolean {
    return this.state.view === "content" || this.state.view === "empty";
  }

  private get link(): LinkState {
    return this.forcedLink ?? this.linkState;
  }

  /** The place draws its last known content (kept on the phone) before any read. */
  showKnown(kind: "content" | "empty" = "content"): void {
    if (this.force) return;
    this.set({ view: kind });
  }

  /** A read the place started begins. Returns its generation for `resolve`/`fail`. */
  start(): number {
    this.stopTimers();
    const g = ++this.gen;
    this.startedAt = this.clock.now();
    this.shownAt = 0;
    this.done = false;
    this.held = null;
    const cold = this.force !== null && this.force !== "offline";
    const keep = this.known && !cold;
    this.set({ slow: false, reading: true, ...(keep ? {} : { view: "none" as WaitView }), ...(cold ? { dim: false } : {}) });
    if (!keep) this.after(motion.wait.quiet, g, () => this.drawPlaceholder());
    this.after(motion.wait.slow, g, () => this.set({ slow: true }));
    if (this.link !== "up") {
      this.waitingForLink = true;
      if (this.link === "away") this.onAway();
    } else {
      this.waitingForLink = false;
      this.after(this.failMs, g, () => this.finish(g, "failed"));
      if (this.force === "error") this.after(FORCED.errorMs, g, () => this.finish(g, "failed"));
    }
    return g;
  }

  /** Waits afresh and asks the place to read again. */
  private again(): void {
    this.start();
    this.options.read?.();
  }

  /** The read returned something (`content`) or succeeded with nothing (`empty`). A stale generation is ignored. */
  resolve(kind: "content" | "empty" = "content", gen?: number): void {
    if (gen !== undefined && gen !== this.gen) return;
    if (this.force === "none" || this.force === "error" || this.force === "offline") return;
    if (this.force === "slow") {
      const left = FORCED.slowMs - (this.clock.now() - this.startedAt);
      if (left > 0 && !this.done) {
        this.held = { kind };
        const g = this.gen;
        this.after(left, g, () => this.held && this.finish(g, this.held.kind));
        return;
      }
    }
    if (this.done && !this.state.reading) {
      // Data that arrives with no read asked for (a push, a refresh underneath): the place shows it.
      this.set({ view: kind, slow: false, dim: false });
      return;
    }
    this.finish(this.gen, kind);
  }

  /** The read failed with an answer (an error frame): the failed state at once, held if a placeholder is up. */
  fail(gen?: number): void {
    if (gen !== undefined && gen !== this.gen) return;
    if (this.force === "none" || this.force === "offline") return;
    if (this.done) return;
    // A failure while the link is down is the link's, not the read's: it waits for the link.
    if (this.link !== "up") {
      this.waitingForLink = true;
      return;
    }
    this.finish(this.gen, "failed");
  }

  /** Try again: an attempt now. While away, `reconnect` is the link's own Try again (the transport's resume). */
  retry(reconnect?: () => void): void {
    if (this.link === "away" && !this.forcedLink) {
      reconnect?.();
      return;
    }
    this.again();
  }

  /** The link to the computer changed. */
  setLink(link: LinkState): void {
    const before = this.link;
    this.linkState = link;
    if (this.forcedLink) return;
    this.applyLink(before);
  }

  /** A dev build forces a state (null: back to the real one); the place reads again in it. */
  setForce(force: WaitForce | null): void {
    if (force === this.force) return;
    const before = this.link;
    this.force = force;
    if (this.forceLinkTimer !== null) this.clock.cancel(this.forceLinkTimer);
    this.forceLinkTimer = null;
    if (force === "offline") {
      this.forcedLink = "connecting";
      this.applyLink(before);
      this.forceLinkTimer = this.clock.later(motion.link.away, () => {
        this.forceLinkTimer = null;
        const was = this.link;
        this.forcedLink = "away";
        this.applyLink(was);
      });
      return;
    }
    const wasForcedLink = this.forcedLink !== null;
    this.forcedLink = null;
    if (wasForcedLink) this.applyLink(before);
    else this.again();
  }

  dispose(): void {
    this.stopTimers();
    if (this.forceLinkTimer !== null) this.clock.cancel(this.forceLinkTimer);
    this.forceLinkTimer = null;
    this.gen++;
  }

  private applyLink(before: LinkState): void {
    const link = this.link;
    if (link === before) return;
    if (link === "up") {
      const again = this.waitingForLink || this.state.view === "away" || this.state.dim || !this.done;
      this.set({ dim: false });
      if (again) this.again();
      return;
    }
    if (!this.done || this.waitingForLink) {
      // A read in flight waits for the link instead of failing.
      this.stopTimers();
      this.waitingForLink = true;
      const g = this.gen;
      const elapsed = this.clock.now() - this.startedAt;
      if (!this.state.slow) this.after(Math.max(0, motion.wait.slow - elapsed), g, () => this.set({ slow: true }));
      if (!this.known && this.state.view !== "placeholder") this.after(Math.max(0, motion.wait.quiet - elapsed), g, () => this.drawPlaceholder());
    }
    if (link === "away") this.onAway();
  }

  private onAway(): void {
    this.stopTimers();
    this.done = true;
    if (this.known) this.set({ slow: false, dim: true, reading: this.waitingForLink });
    else this.set({ view: "away", slow: false, reading: this.waitingForLink });
  }

  private drawPlaceholder(): void {
    if (this.known || this.state.view === "away") return;
    this.shownAt = this.clock.now();
    this.set({ view: "placeholder" });
  }

  private finish(g: number, kind: "content" | "empty" | "failed"): void {
    if (g !== this.gen) return;
    this.done = true;
    this.waitingForLink = false;
    this.held = null;
    const hold = this.state.view === "placeholder" && this.shownAt ? Math.max(0, motion.wait.hold - (this.clock.now() - this.shownAt)) : 0;
    const end = () => {
      if (g !== this.gen) return;
      this.stopTimers();
      // A failure under last known content keeps the content (its refresh failed; what is shown still stands).
      if (kind === "failed" && this.known) this.set({ slow: false, reading: false });
      else this.set({ view: kind, slow: false, dim: false, reading: false });
    };
    if (hold > 0) {
      this.stopTimers();
      this.after(hold, g, end);
    } else end();
  }

  private after(ms: number, g: number, run: () => void): void {
    this.timers.push(this.clock.later(ms, () => g === this.gen && run()));
  }

  private stopTimers(): void {
    for (const t of this.timers) this.clock.cancel(t);
    this.timers = [];
  }

  private set(patch: Partial<WaitState>): void {
    const next = { ...this.state, ...patch };
    if (next.view === this.state.view && next.slow === this.state.slow && next.dim === this.state.dim && next.reading === this.state.reading) return;
    this.state = next;
    this.options.onChange?.(next);
  }
}

/**
 * The link as the person is told it (SPEC §16.1 `link.away`): "connecting" for its first 10 s, then "away",
 * kept between the transport's retries (no flicker back to Connecting) until a socket is up again. A cold
 * start is connecting before it is offline. `onChange` hears each change.
 */
export class LinkWatch {
  private value: LinkState = "connecting";
  private timer: unknown = null;
  private readonly clock: Clock;

  constructor(private readonly onChange?: (link: LinkState) => void, clock: Clock = systemClock) {
    this.clock = clock;
  }

  get current(): LinkState {
    return this.value;
  }

  /** The transport's state changed (`idle` with no computer counts as connecting: there is nothing to be away from yet). */
  update(state: ConnectionState): void {
    if (state === "connected") {
      this.cancel();
      this.set("up");
      return;
    }
    if (this.value === "away") return;
    if (this.value === "up" || this.timer === null) {
      this.set("connecting");
      if (this.timer === null) this.timer = this.clock.later(motion.link.away, () => {
        this.timer = null;
        this.set("away");
      });
    }
  }

  /** A computer switch: the new one starts connecting afresh. */
  reset(): void {
    this.cancel();
    this.set("connecting");
  }

  dispose(): void {
    this.cancel();
  }

  private cancel(): void {
    if (this.timer !== null) this.clock.cancel(this.timer);
    this.timer = null;
  }

  private set(link: LinkState): void {
    if (link === this.value) return;
    this.value = link;
    this.onChange?.(link);
  }
}

/** A pressed action whose result is not true until the computer answers (SPEC §16.2 pattern 5). */
export type ActionPhase = "idle" | "pressed" | "spinning" | "failed";

/**
 * A button that waits (Approve, Answer, Stop, Commit, Connect…): the press alone under `wait.quiet`, then the
 * 20 pt spinner in the button; at the action's bound (`wait.fail`, approve 8 s) it fails with the buttons back.
 * `done()` ends it; the place shows the result (never optimistic).
 */
export class ActionWait {
  private phaseValue: ActionPhase = "idle";
  private timers: unknown[] = [];
  private gen = 0;
  private readonly clock: Clock;

  constructor(private readonly options: { failMs?: number; clock?: Clock; onChange?: (phase: ActionPhase) => void } = {}) {
    this.clock = options.clock ?? systemClock;
  }

  get phase(): ActionPhase {
    return this.phaseValue;
  }

  /** Whether the object's buttons are disabled (the press is out, or the spinner turns). */
  get busy(): boolean {
    return this.phaseValue === "pressed" || this.phaseValue === "spinning";
  }

  press(): number {
    this.stop();
    const g = ++this.gen;
    this.set("pressed");
    this.timers.push(this.clock.later(motion.wait.quiet, () => g === this.gen && this.phaseValue === "pressed" && this.set("spinning")));
    this.timers.push(this.clock.later(this.options.failMs ?? motion.wait.fail, () => g === this.gen && this.set("failed")));
    return g;
  }

  /** The computer answered: back to rest (the place shows what changed). */
  done(gen?: number): void {
    if (gen !== undefined && gen !== this.gen) return;
    this.stop();
    this.set("idle");
  }

  /** The computer refused, or the link dropped: the failed line, the buttons back. */
  fail(gen?: number): void {
    if (gen !== undefined && gen !== this.gen) return;
    this.stop();
    this.set("failed");
  }

  /** The failed line goes (the person acts again, or it is dismissed). */
  clear(): void {
    if (this.phaseValue === "failed") this.set("idle");
  }

  dispose(): void {
    this.stop();
    this.gen++;
  }

  private stop(): void {
    for (const t of this.timers) this.clock.cancel(t);
    this.timers = [];
  }

  private set(phase: ActionPhase): void {
    if (phase === this.phaseValue) return;
    this.phaseValue = phase;
    this.options.onChange?.(phase);
  }
}

/** A call bounded by `ms` (`wait.fail` by default): it rejects with `NoAnswer` when the computer says nothing by then. */
export class NoAnswer extends Error {
  constructor() {
    super("no answer");
    this.name = "NoAnswer";
  }
}

export function bounded<T>(work: Promise<T>, ms: number = motion.wait.fail, clock: Clock = systemClock): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = clock.later(ms, () => reject(new NoAnswer()));
    work.then((value) => (clock.cancel(timer), resolve(value)), (error) => (clock.cancel(timer), reject(error)));
  });
}

/**
 * Optimistic and safe (SPEC §16.2 pattern 7: a switch, a rename, a pin): the result shows at once
 * (`apply(next)`); when the commit rejects or gives no answer by `failMs`, the value goes back (`apply(prev)`)
 * and `onFailed` tells the person (a toast with Try again). Resolves with whether it held.
 */
export function optimistic<T>(input: {
  prev: T;
  next: T;
  apply: (value: T) => void;
  commit: () => Promise<unknown>;
  onFailed?: (error: unknown) => void;
  failMs?: number;
  clock?: Clock;
}): Promise<boolean> {
  const clock = input.clock ?? systemClock;
  input.apply(input.next);
  return new Promise<boolean>((resolve) => {
    let settled = false;
    const settle = (ok: boolean, error?: unknown) => {
      if (settled) return;
      settled = true;
      clock.cancel(timer);
      if (!ok) {
        input.apply(input.prev);
        input.onFailed?.(error);
      }
      resolve(ok);
    };
    const timer = clock.later(input.failMs ?? motion.wait.fail, () => settle(false, new Error("no answer")));
    input.commit().then(() => settle(true), (error) => settle(false, error));
  });
}
