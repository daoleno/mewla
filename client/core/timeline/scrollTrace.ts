/**
 * A chat list's scroll trace: every scroll command the chat issues (who, why, target), every change of its rows'
 * identities as NSH's List sees it (the splice its keyed diff makes), every scroll report, and the footer room.
 * Off unless a sink is set: `nsh test` (FakeDaemonApp) posts it to the test, a dev build can log it.
 */
export type ScrollTraceEntry = { at: number; scope: string; kind: string } & Record<string, unknown>;

let sink: ((entry: ScrollTraceEntry) => void) | null = null;

export function setScrollTraceSink(next: ((entry: ScrollTraceEntry) => void) | null): void {
  sink = next;
}

export function scrollTraceOn(): boolean {
  return sink !== null;
}

export function traceScroll(scope: string, kind: string, data: Record<string, unknown> = {}): void {
  if (!sink) return;
  try {
    sink({ at: Date.now(), scope, kind, ...data });
  } catch {
    // A trace never breaks the chat.
  }
}

/**
 * The splice NSH 0.5.0's List makes when its `each` changes (crates/nsh-js/src/list.rs `items_run`): the common
 * start and end by key, and everything between removed and inserted again (measurements lost, sizes estimated).
 */
export function keyedSplice(old: readonly string[], next: readonly string[]): { at: number; removed: number; inserted: number } | null {
  if (old.length === 0) return null;
  const max = Math.min(old.length, next.length);
  let start = 0;
  while (start < max && old[start] === next[start]) start += 1;
  let end = 0;
  while (end < max - start && old[old.length - 1 - end] === next[next.length - 1 - end]) end += 1;
  const removed = old.length - start - end;
  const inserted = next.length - start - end;
  return removed > 0 || inserted > 0 ? { at: start, removed, inserted } : null;
}
