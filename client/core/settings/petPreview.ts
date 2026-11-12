/**
 * The pet's states and the moves between them (SPEC §5): one clip per state, and the hop out of the
 * nap or back into it in between. Clip lengths and canvases are the pack's (`core/pets/manifest`).
 */

export type PetState = "homeless" | "offline" | "waking" | "idle" | "working" | "delegating" | "attention" | "delivered";

const NAPPING = new Set<PetState>(["homeless", "offline", "idle"]);

/** Where a state leaves the pet: napping (asleep, greyed or gone), or awake on its feet. */
export function petInSeal(state: PetState): boolean {
  return NAPPING.has(state);
}

/**
 * The clip to play on the way from one state to another before the new state's own:
 * going_back on the way into the nap, waking on the way out; null on the same side.
 */
export function petTransition(from: PetState, to: PetState): "waking" | "going_back" | null {
  if (from === to || to === "waking" || from === "homeless" || to === "homeless") return null;
  const fromNap = petInSeal(from);
  const toNap = petInSeal(to);
  if (fromNap && !toNap) return "waking";
  if (!fromNap && toNap) return "going_back";
  return null;
}
