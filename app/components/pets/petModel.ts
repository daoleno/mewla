import type { ImageSourcePropType } from "react-native";
import type { BrainCatState } from "../mewla/brainCatState";

/**
 * A pack has one animated WebP per Brain state, plus going_back: the hop back
 * into the seal. waking is the hop out. One-shots hold their last frame.
 */
export type PetClipName = BrainCatState | "going_back";

export interface PetClip {
  source: ImageSourcePropType;
  loop: boolean;
  /** One pass through every frame. */
  durationMs: number;
}

export interface PetPack {
  id: string;
  name: { en: string; zh: string };
  portrait: ImageSourcePropType;
  clips: Record<PetClipName, PetClip>;
}

/**
 * Where each state leaves the pet: in the seal (asleep, greyed, or gone), or
 * out on its feet (attention waits on its feet, perched on Work). waking ends
 * on its feet.
 */
const IN_SEAL = new Set<BrainCatState>(["homeless", "offline", "idle"]);

export function petInSeal(state: BrainCatState): boolean {
  return IN_SEAL.has(state);
}

/**
 * The clip to play on the way from one state to another before the new
 * state's own clip: going_back on the way into the seal, waking on the way
 * out. Null when the pet stays on the same side, or the new clip is the hop.
 */
export function petTransition(from: BrainCatState, to: BrainCatState): PetClipName | null {
  if (from === to || to === "waking" || from === "homeless") return null;
  const fromSeal = petInSeal(from);
  const toSeal = petInSeal(to);
  if (fromSeal && !toSeal) return "waking";
  if (!fromSeal && toSeal) return "going_back";
  return null;
}

export function findPetPack(packs: readonly PetPack[], id: string | null | undefined, fallbackId: string): PetPack {
  return packs.find((pack) => pack.id === id) ?? packs.find((pack) => pack.id === fallbackId) ?? packs[0];
}
