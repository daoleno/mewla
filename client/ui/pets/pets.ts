import { signal, Storage } from "nsh";
import { DEFAULT_PET_ID, PET_MANIFEST, resolvePetId, type PetEntry } from "../../core/pets/manifest";
import { PET_ART, type PetArt } from "./assets";

/** A pet: its manifest entry (name, clip lengths, canvases) and its files. */
export type Pet = PetEntry & { art: PetArt };

/** The pets offered, in the manifest's order (the default first): those whose art the app ships. */
export const PETS: readonly Pet[] = PET_MANIFEST.filter((entry) => entry.id in PET_ART).map((entry) => ({ ...entry, art: PET_ART[entry.id]! }));

const DEFAULT_PET = PETS.find((p) => p.id === DEFAULT_PET_ID) ?? PETS[0]!;

/** The Expo app's key, so the pet chosen there stays chosen. */
const PET_KEY = "mewla:pet_preference";

export function findPet(id: string | null | undefined): Pet | undefined {
  return PETS.find((p) => p.id === id);
}

/** A pet by id, or the default for one that was removed (a friend's avatar, a stored choice). */
export function petOrDefault(id: string | null | undefined): Pet {
  return findPet(resolvePetId(id)) ?? DEFAULT_PET;
}

const chosen = signal(DEFAULT_PET.id);

/** The pet shown wherever Brain appears, one per phone; Friends names your agent after it. */
export function pet(): Pet {
  return petOrDefault(chosen());
}

let restored = false;

/** The stored choice; a removed pet stays the default (removed for everyone). */
export function restorePet(): void {
  if (restored) return;
  restored = true;
  void Storage.getItem(PET_KEY)
    .then((stored) => {
      if (findPet(stored)) chosen.set(stored!);
    })
    .catch(() => {});
}

export function choosePet(id: string): void {
  if (!findPet(id) || id === chosen()) return;
  chosen.set(id);
  void Storage.setItem(PET_KEY, id).catch(() => {});
}
