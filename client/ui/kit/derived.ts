import { effect, signal, untracked, type Accessor, type SignalOptions } from "nsh";
import { prepareEventRows } from "../../core/timeline/model";
import type { CodexConversationEvent } from "../../core/conversation/conversation";

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

/** How long one call spends making a thread's new rows: some 8 times this on a slow phone, well inside NSH's 2 s. */
export const PREPARE_SLICE_MS = 40;

/**
 * A thread's rows, `derived`, with the rows of its new events made first a slice at a time, each slice a call of its own
 * (core/timeline/model.ts `prepareEventRows`). Until they all are, the rows stay as they were (`initial` before any);
 * then the thread is put together from what is made, in one cheap call. Made in that one call, thousands of new
 * events took it past NSH's 2 s: NSH cut it off, and every run after it started over.
 */
export function preparedRows<T>(events: () => readonly CodexConversationEvent[], make: () => T, initial: T): Accessor<T> {
  const slice = signal(0);
  let next: ReturnType<typeof setTimeout> | null = null;
  let last = initial;
  return derived(() => {
    slice();
    if (!prepareEventRows(events(), Date.now() + PREPARE_SLICE_MS)) {
      next ??= setTimeout(() => {
        next = null;
        slice.set(untracked(slice) + 1);
      }, 0);
      return last;
    }
    last = make();
    return last;
  });
}
