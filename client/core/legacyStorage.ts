import type { KeyValueStore } from "./platform";
import { SERVER_KEYS } from "./servers";

/** The Expo app's AsyncStorage, as the host reads it on the first start after the upgrade. */
export interface LegacyStorageSource {
  /** The Mewla entries, or null once copied (or with no Expo app before this one). */
  entries(): Promise<Record<string, string> | null>;
  /** Called after the copy; the host stops offering the entries. */
  copied(): Promise<void>;
}

/** NSH storage holds about 5 MB; whatever the Expo app cached beyond the essentials may not fit. */
export const LEGACY_COPY_BUDGET = 3_500_000;

const DRAFT_PREFIX = "mewla.draft.v1:";
const rank = (key: string) =>
  (Object.values(SERVER_KEYS) as string[]).includes(key) ? 0 : key.startsWith(DRAFT_PREFIX) ? 1 : 2;

/**
 * Copies the Expo app's entries into this app's storage once, before anything reads it: the
 * computers and the current one first, then drafts, then the rest, as far as the budget goes.
 * An entry this app already wrote wins. Returns the keys copied.
 */
export async function copyLegacyStorage(
  source: LegacyStorageSource,
  into: KeyValueStore,
  budget = LEGACY_COPY_BUDGET,
): Promise<string[]> {
  const entries = await source.entries();
  if (!entries) return [];
  const copied: string[] = [];
  let left = budget;
  const keys = Object.keys(entries).sort((a, b) => rank(a) - rank(b) || a.localeCompare(b));
  for (const key of keys) {
    const value = entries[key];
    const size = key.length + value.length;
    if (size > left || (await into.getItem(key)) !== null) continue;
    await into.setItem(key, value);
    left -= size;
    copied.push(key);
  }
  await source.copied();
  return copied;
}
