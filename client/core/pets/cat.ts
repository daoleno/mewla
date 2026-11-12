import type { PetState } from "../settings/petPreview";

/** What the header cat reads (the tray's state, and what it cannot say on its own). */
export interface CatInput {
  /** The tray's state kind (`core/tray/model` StateKind). */
  kind: string;
  /** Brain is starting on a connected computer (Brain's surface is `waking`). */
  starting: boolean;
  /** Requests waiting on you. */
  needs: number;
  /** Brain's own turn is running. */
  brainTurn: boolean;
  /** The Running rows the state control counts (`TrayModel.runningCount`): quiet queued and waiting Work left out. */
  running: number;
}

/**
 * The header cat's state (SPEC §5), highest first: no computer, offline (pair again and Brain can't
 * start too), connecting (Brain starting too), needs you, Brain's turn, Workers running, quiet. The
 * tray words "N need you" over "Starting…"; the cat wakes first.
 */
export function catState(input: CatInput): PetState {
  switch (input.kind) {
    case "none": return "homeless";
    case "offline":
    case "pair":
    case "cant_start": return "offline";
    case "connecting":
    case "starting": return "waking";
  }
  if (input.starting) return "waking";
  if (input.needs > 0) return "attention";
  if (input.brainTurn) return "working";
  if (input.running > 0 || input.kind === "running") return "delegating";
  return "idle";
}

/**
 * Whether a Work came back done since the last look: a result unread now that was not before.
 * The first look (`previous` null: the app opened, the computer switched) only takes stock.
 */
export function deliveredSince(previous: ReadonlySet<string> | null, unread: ReadonlySet<string>): boolean {
  if (!previous) return false;
  for (const id of unread) if (!previous.has(id)) return true;
  return false;
}
