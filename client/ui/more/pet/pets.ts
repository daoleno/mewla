import { signal, Storage } from "nsh";
import type { ImageAsset } from "nsh";
import p01 from "../assets/pets/p01/portrait.webp";
import p02 from "../assets/pets/p02/portrait.webp";
import p03 from "../assets/pets/p03/portrait.webp";
import p04 from "../assets/pets/p04/portrait.webp";
import p05 from "../assets/pets/p05/portrait.webp";
import p06 from "../assets/pets/p06/portrait.webp";
import p07 from "../assets/pets/p07/portrait.webp";
import p08 from "../assets/pets/p08/portrait.webp";
import p09 from "../assets/pets/p09/portrait.webp";
import p10 from "../assets/pets/p10/portrait.webp";

/** The pet shown wherever Brain appears, one per phone; Friends names your agent after it. */
export interface Pet {
  id: string;
  name: string;
  portrait: ImageAsset;
}

export const PETS: readonly Pet[] = [
  { id: "p05", name: "Dango", portrait: p05 },
  { id: "p01", name: "Mochi", portrait: p01 },
  { id: "p02", name: "Miso", portrait: p02 },
  { id: "p03", name: "Pip", portrait: p03 },
  { id: "p04", name: "Nori", portrait: p04 },
  { id: "p06", name: "Kiko", portrait: p06 },
  { id: "p07", name: "Lumi", portrait: p07 },
  { id: "p08", name: "Poko", portrait: p08 },
  { id: "p09", name: "Tama", portrait: p09 },
  { id: "p10", name: "Sumi", portrait: p10 },
];

export const DEFAULT_PET_ID = "p05";

/** The Expo app's key, so the pet chosen there stays chosen. */
const PET_KEY = "mewla:pet_preference";

export function findPet(id: string | null | undefined): Pet | undefined {
  return PETS.find((pet) => pet.id === id);
}

const chosen = signal(DEFAULT_PET_ID);

export function pet(): Pet {
  return findPet(chosen()) ?? PETS[0];
}

let restored = false;

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
