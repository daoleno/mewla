import { effect, signal, untracked, type Accessor, type SignalOptions } from "nsh";

/**
 * A value made from signals, as `computed` makes one, that a failed run does not end.
 *
 * Under NSH 0.5.0 and 0.5.1 a `computed` whose run throws, any error or the engine cutting off a call into JS that passed its
 * 2 s budget ("Error: interrupted"), never tells what reads it of a change again, for as long as it lives (the
 * probe in derived.test.tsx). A thread's rows were one: a phone that took more than 2 s over one update of Brain's
 * thread cut off its projection, and the thread drew nothing new, not the reply, not the person's next message,
 * not after a reconnect, until the app started again (owner 11-14, "Working · 4m" over an answer the terminal had).
 * An effect whose run fails stays subscribed, so here the value is an effect's: a run that fails keeps the last
 * value, and the next change makes it anew.
 *
 * The first value is made at once, untracked, and once more when the effect first runs (to learn what it reads):
 * use it where that is cheap at creation or where a value that may be stale for a moment is never worse than one
 * that stops.
 */
export function derived<T>(make: () => T, options: SignalOptions<T> = {}): Accessor<T> {
  const value = signal<T>(untracked(make), options);
  effect(() => {
    value.set(make());
  });
  return () => value();
}
