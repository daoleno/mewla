import { computed, signal, Storage, untracked } from "nsh";
import { appUpdateMark, catUpdateHintOf, parseSeenUpdates, type SeenUpdates } from "../../core/update/hint";
import { computerOptionalUpdate } from "../computer/state";
import { current } from "../session";
import { appUpdateOwner as owner } from "./update";

/**
 * The light update hint (core/update/hint): the ink dot on home's cat and on the computer page's
 * Settings row, from the update state the app already has (no check of its own). What the person
 * has seen is kept on the phone under `mewla:update_seen`.
 */

const SEEN_KEY = "mewla:update_seen";
const seen = signal<SeenUpdates | null>(null);
void Storage.getItem(SEEN_KEY)
  .then((raw) => seen.set({ ...parseSeenUpdates(raw), ...untracked(seen) }))
  .catch(() => seen.set({}));

function remember(next: SeenUpdates): void {
  seen.set(next);
  void Storage.setItem(SEEN_KEY, JSON.stringify(next)).catch(() => {});
}

/** Settings showed this app's update row: the cat lets it go for this version. */
export function markAppUpdateSeen(): void {
  const mark = appUpdateMark(untracked(() => owner().state()));
  const now = untracked(seen) ?? {};
  if (mark && now.app !== mark) remember({ ...now, app: mark });
}

/** The computer page showed the current computer's update row: the cat lets it go for this version. */
export function markComputerUpdateSeen(): void {
  const latest = untracked(computerOptionalUpdate);
  const id = untracked(current)?.id;
  const now = untracked(seen) ?? {};
  if (latest && id && now.computers?.[id] !== latest) remember({ ...now, computers: { ...now.computers, [id]: latest } });
}

/** The ink dot on home's cat. */
export const catUpdateHint = computed(() =>
  catUpdateHintOf({ app: appUpdateMark(owner().state()), computer: computerOptionalUpdate(), serverId: current()?.id ?? null, seen: seen() }),
);
