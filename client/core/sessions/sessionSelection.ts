/** From the Expo app's services/sessionSelection.ts. */

/**
 * Telegram-style multi-selection over the Sessions list.
 *
 * Selection is keyed by the app's canonical stable Session key
 * (`makeSessionKey(serverId, workerId)`), never by row index, display name or
 * order. That is what lets selection survive list reorder, section moves and
 * live updates; it is pruned only when the authoritative Session row
 * disappears from the daemon-backed store.
 */
export type SessionSelection = ReadonlySet<string>;

export const EMPTY_SESSION_SELECTION: SessionSelection = new Set<string>();

/** Add or remove one Session key. Always returns a new Set. */
export function toggleSessionSelection(
  selection: SessionSelection,
  sessionKey: string,
): SessionSelection {
  const next = new Set(selection);
  if (next.has(sessionKey)) {
    next.delete(sessionKey);
  } else {
    next.add(sessionKey);
  }
  return next;
}

/**
 * Keep only keys whose authoritative row still exists. Returns the same set
 * reference when nothing was pruned so React state updates can bail out.
 */
export function pruneSessionSelection(
  selection: SessionSelection,
  authoritativeSessionKeys: Iterable<string>,
): SessionSelection {
  if (selection.size === 0) {
    return selection;
  }
  const known = new Set(authoritativeSessionKeys);
  const pruned = new Set<string>();
  for (const sessionKey of selection) {
    if (known.has(sessionKey)) {
      pruned.add(sessionKey);
    }
  }
  return pruned.size === selection.size ? selection : pruned;
}

export function countSessionSelection(selection: SessionSelection): number {
  return selection.size;
}

export function selectionCountLabel(count: number): string {
  return count === 1 ? "1 selected" : `${count} selected`;
}
