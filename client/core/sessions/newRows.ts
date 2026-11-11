/** More new rows than this at once is a reload, not news: nothing fades. */
export const MAX_ANIMATED_NEW_ROWS = 40;

const NO_ROWS: ReadonlySet<string> = new Set();

/**
 * The Session keys that were not in the list the last time it changed (they fade
 * in). None for the first list, an empty one, or when nothing carried over from
 * the last list (another computer's Sessions replaced it), so opening the page or
 * switching computers animates nothing.
 */
export function newRowKeys(known: ReadonlySet<string> | null, keys: readonly string[]): ReadonlySet<string> {
  if (!known || known.size === 0) return NO_ROWS;
  if (!keys.some((key) => known.has(key))) return NO_ROWS;
  const added = keys.filter((key) => !known.has(key));
  return added.length === 0 || added.length > MAX_ANIMATED_NEW_ROWS ? NO_ROWS : new Set(added);
}
