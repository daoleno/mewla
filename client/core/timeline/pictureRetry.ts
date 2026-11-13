/**
 * How a chat picture reads again after a read that did not answer (SPEC §16.2): a read the link interrupted
 * waits for the link and is no failure; with the link up it reads again after 1, 2, 4 and 8 s, and only then
 * shows its refresh glyph. An answer that it is no picture (the computer said so) fails at once.
 */
export const PICTURE_RETRY_MS = [1_000, 2_000, 4_000, 8_000] as const;

/** What a picture does after its `failures`-th read that did not answer: read again after that many ms, wait for the link, or fail. */
export function pictureRetry(failures: number, linkUp: boolean): { wait: "link" } | { after: number } | { fail: true } {
  if (!linkUp) return { wait: "link" };
  const after = PICTURE_RETRY_MS[failures - 1];
  return after === undefined ? { fail: true } : { after };
}

/** A picture read over the computer's link (`codex_asset`) answers in this long: a phone photo over the relay, behind the thread's own news. */
export const PICTURE_READ_MS = 30_000;
