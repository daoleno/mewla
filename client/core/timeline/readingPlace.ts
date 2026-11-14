import { platform } from "../platform";
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

/** The scope Brain's thread keeps its reading place under, on one computer. */
export function brainThreadReadingScope(serverId: string, threadId: string): string {
  return `brain:${serverId}:chat:${threadId}`;
}

/** The index of the row a chat was read to, if it is still there. */
export function readingPlaceIndex(rows: readonly { id: string }[], key: string | undefined): number | undefined {
  if (key === undefined) return undefined;
  const index = rows.findIndex((row) => row.id === key);
  return index < 0 ? undefined : index;
}

/** Kept in storage under this key: `{ [scope]: { key, at } }`, `at` when the app last kept it. */
export const READING_PLACES_KEY = "mewla:v1:reading_places";
/**
 * How long a reading place outlives the app: a reload (the dev channel's, or the system making the app again
 * after it was in the background) within this of the app last keeping it comes back to it; a start after
 * longer opens chats at their end. While the app runs, a place never expires.
 */
export const READING_PLACE_KEEP_MS = 15 * 60_000;
const SAVE_AFTER_MS = 500;

/**
 * Where each chat was read to (its first row on screen, by key), by its scope. Kept here, not beside the list,
 * so a hot patch (which makes the patched module's Maps anew) keeps it, and in storage, so the app started
 * again (owner 11-13: a reload while reading opened Brain's thread at its newest row) keeps it too.
 */
const places = new Map<string, string>();
let loading: Promise<void> | null = null;
// Nothing is kept before the places from before this start are read: it would write over them.
let loaded = false;
let saveTimer: ReturnType<typeof setTimeout> | null = null;

/** Reads the places kept from before this start (kept less than READING_PLACE_KEEP_MS ago), once. */
export function loadReadingPlaces(now: () => number = Date.now): Promise<void> {
  loading ??= (async () => {
    try {
      const raw = await platform().storage.getItem(READING_PLACES_KEY);
      const saved = raw ? (JSON.parse(raw) as Record<string, { key?: unknown; at?: unknown }>) : {};
      for (const [scope, kept] of Object.entries(saved)) {
        if (places.has(scope) || typeof kept?.key !== "string" || typeof kept.at !== "number") continue;
        if (now() - kept.at < READING_PLACE_KEEP_MS) places.set(scope, kept.key);
      }
    } catch {
      // Nothing kept, or unreadable: chats open at their end.
    }
    loaded = true;
  })();
  return loading;
}

/** The row a chat was read to, if it was read back from its end. */
export function readingPlaceOf(scope: string): string | undefined {
  return places.get(scope);
}

/** The person read a chat back to `key` (undefined: they are at its end again). Kept shortly after, not on every scroll. */
export function rememberReadingPlace(scope: string, key: string | undefined, now: () => number = Date.now): void {
  if (key === undefined ? !places.delete(scope) : places.get(scope) === key) return;
  if (key !== undefined) places.set(scope, key);
  if (!saveTimer) saveTimer = setTimeout(() => keepReadingPlaces(now), SAVE_AFTER_MS);
}

/** Keeps every place in storage now, stamped now (also when the app leaves the front: it may not come back to this run). */
export function keepReadingPlaces(now: () => number = Date.now): void {
  if (saveTimer) clearTimeout(saveTimer);
  saveTimer = null;
  if (!loaded) return;
  const at = now();
  const kept = Object.fromEntries([...places].map(([scope, key]) => [scope, { key, at }]));
  try {
    void platform().storage.setItem(READING_PLACES_KEY, JSON.stringify(kept)).catch(() => undefined);
  } catch {
    // No platform (a test that ended): nothing to keep.
  }
}

/** For tests: forget everything kept in memory. */
export function resetReadingPlacesForTest(): void {
  places.clear();
  loading = null;
  loaded = false;
  if (saveTimer) clearTimeout(saveTimer);
  saveTimer = null;
}
