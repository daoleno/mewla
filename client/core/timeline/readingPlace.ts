/**
 * Where a chat was read to, kept as the first row on screen (its key), not as a pixel offset.
 * A reopened list measures its rows as they show; an offset saved from a measured list can land
 * past what the new one has measured, where no row is mounted (a Claude Session's chat opened
 * blank after a long reply, FAILURES R4#9). A row is found again by its index.
 */
export function readingPlace(tokens: readonly { key: string; index: number; isViewable: boolean }[]): string | undefined {
  let first: { key: string; index: number } | undefined;
  for (const token of tokens) if (token.isViewable && (!first || token.index < first.index)) first = token;
  return first?.key;
}

/** The index of the row a chat was read to, if it is still there. */
export function readingPlaceIndex(rows: readonly { id: string }[], key: string | undefined): number | undefined {
  if (key === undefined) return undefined;
  const index = rows.findIndex((row) => row.id === key);
  return index < 0 ? undefined : index;
}
