import type { AppUpdateState } from "./appUpdate";

/**
 * The light hint that an update waits (owner, 2026-11-14): an ink dot on home's cat while a new
 * update of this app or of the current computer's Mewla has not been looked at yet, and the same
 * dot on the computer page's Settings row while this app's update waits. Red stays "your move":
 * a required update is the red dot (`catNeedsYouOf`), and the red dot wins over this one.
 *
 * The cat's dot goes once the person has seen that update's row (Settings for this app, the
 * computer page for the computer), not only once it is installed: home stays calm for someone who
 * chose to wait, and the next version lights it again. The rows on the way keep their mark until
 * the update is in, so it is still found.
 */

/** What the person has seen: this app's update mark, and each computer's newer Mewla by server id. */
export interface SeenUpdates {
  app?: string;
  computers?: Record<string, string>;
}

/** Which waiting update of this app it is (TestFlight: version and build); null while none waits. */
export function appUpdateMark(state: AppUpdateState): string | null {
  if (state.kind === "testflightAvailable") return `${state.update.version}+${state.update.buildNumber}`;
  return "update" in state ? state.update.version : null;
}

export interface UpdateHintInput {
  /** `appUpdateMark` of this app's update state. */
  app: string | null;
  /** The current computer's optional update (`optionalUpdate`), while connected. */
  computer: string | null;
  serverId: string | null;
  /** Null until it has been read, so the dot never flashes at launch. */
  seen: SeenUpdates | null;
}

/** Whether home's cat carries the ink dot. */
export function catUpdateHintOf(input: UpdateHintInput): boolean {
  const seen = input.seen;
  if (!seen) return false;
  if (input.app && seen.app !== input.app) return true;
  return Boolean(input.computer && input.serverId && seen.computers?.[input.serverId] !== input.computer);
}

/** What is stored, read leniently: anything of the wrong shape reads as nothing seen. */
export function parseSeenUpdates(raw: string | null): SeenUpdates {
  if (!raw) return {};
  try {
    const value = JSON.parse(raw) as unknown;
    if (!value || typeof value !== "object" || Array.isArray(value)) return {};
    const record = value as Record<string, unknown>;
    const out: SeenUpdates = {};
    if (typeof record.app === "string") out.app = record.app;
    if (record.computers && typeof record.computers === "object" && !Array.isArray(record.computers)) {
      const computers: Record<string, string> = {};
      for (const [id, mark] of Object.entries(record.computers as Record<string, unknown>)) if (typeof mark === "string") computers[id] = mark;
      out.computers = computers;
    }
    return out;
  } catch {
    return {};
  }
}
