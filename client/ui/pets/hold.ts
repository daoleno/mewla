import { signal } from "nsh";

/** How long after the last scroll the person still counts as scrolling. */
const SCROLL_QUIET_MS = 600;

/** The person is scrolling a thread: the cat holds still (SPEC §5 "never during scroll or typing"). */
export const scrolling = signal(false);
let quiet: ReturnType<typeof setTimeout> | null = null;

/** A thread scrolled (its list's onScroll): true now, false once it has been quiet a moment. */
export function noteScroll(): void {
  if (!scrolling()) scrolling.set(true);
  if (quiet) clearTimeout(quiet);
  quiet = setTimeout(() => {
    quiet = null;
    scrolling.set(false);
  }, SCROLL_QUIET_MS);
}
