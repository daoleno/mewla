const warned = new Set<string>();

/** The text to show or log for a failure of unknown shape. */
export function failureMessage(error: unknown, fallback = "The computer did not answer."): string {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === "string" && error) return error;
  return fallback;
}

/**
 * Logs a failure the first time it is seen for `scope`, so a loop that keeps
 * failing the same way leaves one line instead of a flood. Returns whether it
 * logged.
 */
export function warnOnce(scope: string, error: unknown): boolean {
  const message = failureMessage(error);
  const key = `${scope}:${message}`;
  if (warned.has(key)) return false;
  warned.add(key);
  console.warn(`[${scope}] ${message}`);
  return true;
}

/** Test seam: forget what has been logged. */
export function resetWarnOnce() {
  warned.clear();
}
