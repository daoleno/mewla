import data from "./manifest.json";

/**
 * The pets (soft-final SPEC §5, §13): six cats, each in its own medium. `manifest.json` is the pets
 * pack's manifest as MewlaPetsSix delivers it (worklog/mewla-pets-six-1112/delivery/manifest.json),
 * without its file lists and provenance notes: per pet its name, frame, hop bleed, each clip's canvas
 * size, frames, delays, loop and bleed, and each still's source frame. A pack's files go in
 * `client/ui/assets/pets/<id>/` under the clip names; adding a pet is its folder, its entry in the
 * JSON and its imports in `ui/pets/assets.ts`. Removed pets are removed for everyone: a stored id
 * that is not a pet falls back to the default.
 */

/** The clips a pet plays, by the client's names (SPEC §5). */
export type PetClipName = "idle" | "waking" | "going_back" | "working" | "delegating" | "attention" | "delivered" | "offline";

/** The states with a still (`<state>-still.webp`): one frame of the state's clip, cut tight (90 %). */
export type PetStillName = "idle" | "working" | "delegating" | "attention" | "waking" | "offline";

export const PET_CLIPS: readonly PetClipName[] = ["idle", "waking", "going_back", "working", "delegating", "attention", "delivered", "offline"];
export const PET_STILLS: readonly PetStillName[] = ["idle", "working", "delegating", "attention", "waking", "offline"];

export interface PetClipSpec {
  /** The canvas's side in pixels (the frame, or the frame plus the bleed on each side). */
  size: number;
  frames: number;
  delays_ms: number[];
  /** One pass through every frame. */
  total_ms: number;
  loop: boolean;
  /** How far the square canvas reaches past the pet's box on each side, as a share of the box (0: the box). */
  bleed: number;
}

export interface PetEntry {
  id: string;
  name: string;
  /** The box's side in pixels (256 for Dango, 192 for the others). */
  frame: number;
  hopBleed: number;
  clips: Record<PetClipName, PetClipSpec>;
  stills: Record<PetStillName, { size: number; from_frame: number }>;
}

interface ManifestFile {
  version: number;
  default: string;
  remove: string[];
  removed_ids_fall_back_to: string;
  pets: Record<string, Omit<PetEntry, "id" | "clips"> & { clips: Record<PetClipName, PetClipSpec> & Record<string, unknown> }>;
}

const manifest = data as unknown as ManifestFile;

export const DEFAULT_PET_ID = manifest.default;

/** Every pet, in the manifest's order (the default first). */
export const PET_MANIFEST: readonly PetEntry[] = Object.entries(manifest.pets).map(([id, pet]) => ({
  id,
  name: pet.name,
  frame: pet.frame,
  hopBleed: pet.hopBleed,
  clips: Object.fromEntries(PET_CLIPS.map((clip) => [clip, pet.clips[clip]])) as Record<PetClipName, PetClipSpec>,
  stills: pet.stills,
}));

export function petEntry(id: string | null | undefined): PetEntry | undefined {
  return PET_MANIFEST.find((entry) => entry.id === id);
}

/** The pet a stored choice gives: itself if it is a pet, else the default (a removed pet is removed for everyone). */
export function resolvePetId(stored: string | null | undefined): string {
  return petEntry(stored) ? stored! : DEFAULT_PET_ID;
}

/**
 * Where a clip's image sits around a box of `size` points, the box's top-left at 0, 0: a clip on the
 * bleed canvas is larger than the box and centred on it, so the cat keeps its size and may draw
 * past the box (above it in the header: nothing there clips it).
 */
export function clipFrame(spec: Pick<PetClipSpec, "bleed">, size: number): { left: number; top: number; width: number; height: number } {
  const bleed = spec.bleed * size;
  if (!bleed) return { left: 0, top: 0, width: size, height: size };
  return { left: -bleed, top: -bleed, width: size + 2 * bleed, height: size + 2 * bleed };
}
