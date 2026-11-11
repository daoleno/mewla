import { signal, Storage } from "nsh";
import type { ImageAsset } from "nsh";
import type { CLIP_TIMING } from "../../../core/settings/petPreview";
import p01 from "../assets/pets/p01/portrait.webp";
import p01delegating from "../assets/pets/p01/delegating.webp";
import p01delivered from "../assets/pets/p01/delivered.webp";
import p01going from "../assets/pets/p01/going_back.webp";
import p01idle from "../assets/pets/p01/idle.webp";
import p01waking from "../assets/pets/p01/waking.webp";
import p01working from "../assets/pets/p01/working.webp";
import p02 from "../assets/pets/p02/portrait.webp";
import p02delegating from "../assets/pets/p02/delegating.webp";
import p02delivered from "../assets/pets/p02/delivered.webp";
import p02going from "../assets/pets/p02/going_back.webp";
import p02idle from "../assets/pets/p02/idle.webp";
import p02waking from "../assets/pets/p02/waking.webp";
import p02working from "../assets/pets/p02/working.webp";
import p03 from "../assets/pets/p03/portrait.webp";
import p03delegating from "../assets/pets/p03/delegating.webp";
import p03delivered from "../assets/pets/p03/delivered.webp";
import p03going from "../assets/pets/p03/going_back.webp";
import p03idle from "../assets/pets/p03/idle.webp";
import p03waking from "../assets/pets/p03/waking.webp";
import p03working from "../assets/pets/p03/working.webp";
import p04 from "../assets/pets/p04/portrait.webp";
import p04delegating from "../assets/pets/p04/delegating.webp";
import p04delivered from "../assets/pets/p04/delivered.webp";
import p04going from "../assets/pets/p04/going_back.webp";
import p04idle from "../assets/pets/p04/idle.webp";
import p04waking from "../assets/pets/p04/waking.webp";
import p04working from "../assets/pets/p04/working.webp";
import p05 from "../assets/pets/p05/portrait.webp";
import p05delegating from "../assets/pets/p05/delegating.webp";
import p05delivered from "../assets/pets/p05/delivered.webp";
import p05going from "../assets/pets/p05/going_back.webp";
import p05idle from "../assets/pets/p05/idle.webp";
import p05waking from "../assets/pets/p05/waking.webp";
import p05working from "../assets/pets/p05/working.webp";
import p06 from "../assets/pets/p06/portrait.webp";
import p06delegating from "../assets/pets/p06/delegating.webp";
import p06delivered from "../assets/pets/p06/delivered.webp";
import p06going from "../assets/pets/p06/going_back.webp";
import p06idle from "../assets/pets/p06/idle.webp";
import p06waking from "../assets/pets/p06/waking.webp";
import p06working from "../assets/pets/p06/working.webp";
import p07 from "../assets/pets/p07/portrait.webp";
import p07delegating from "../assets/pets/p07/delegating.webp";
import p07delivered from "../assets/pets/p07/delivered.webp";
import p07going from "../assets/pets/p07/going_back.webp";
import p07idle from "../assets/pets/p07/idle.webp";
import p07waking from "../assets/pets/p07/waking.webp";
import p07working from "../assets/pets/p07/working.webp";
import p08 from "../assets/pets/p08/portrait.webp";
import p08delegating from "../assets/pets/p08/delegating.webp";
import p08delivered from "../assets/pets/p08/delivered.webp";
import p08going from "../assets/pets/p08/going_back.webp";
import p08idle from "../assets/pets/p08/idle.webp";
import p08waking from "../assets/pets/p08/waking.webp";
import p08working from "../assets/pets/p08/working.webp";
import p09 from "../assets/pets/p09/portrait.webp";
import p09delegating from "../assets/pets/p09/delegating.webp";
import p09delivered from "../assets/pets/p09/delivered.webp";
import p09going from "../assets/pets/p09/going_back.webp";
import p09idle from "../assets/pets/p09/idle.webp";
import p09waking from "../assets/pets/p09/waking.webp";
import p09working from "../assets/pets/p09/working.webp";
import p10 from "../assets/pets/p10/portrait.webp";
import p10delegating from "../assets/pets/p10/delegating.webp";
import p10delivered from "../assets/pets/p10/delivered.webp";
import p10going from "../assets/pets/p10/going_back.webp";
import p10idle from "../assets/pets/p10/idle.webp";
import p10waking from "../assets/pets/p10/waking.webp";
import p10working from "../assets/pets/p10/working.webp";

/** The animated clips a pet has (animated WebP, one per Brain state the preview plays, and the hops). */
export type PetClips = Record<keyof typeof CLIP_TIMING, ImageAsset>;

/** The pet shown wherever Brain appears, one per phone; Friends names your agent after it. */
export interface Pet {
  id: string;
  name: string;
  portrait: ImageAsset;
  clips: PetClips;
  /** How far its hops draw past its box (a share of the box); one pack's ring is wider. */
  hopBleed: number;
}

export const PETS: readonly Pet[] = [
  { id: "p05", name: "Dango", portrait: p05, hopBleed: 0.2812, clips: { delegating: p05delegating, working: p05working, delivered: p05delivered, idle: p05idle, waking: p05waking, going_back: p05going } },
  { id: "p01", name: "Mochi", portrait: p01, hopBleed: 0.2812, clips: { delegating: p01delegating, working: p01working, delivered: p01delivered, idle: p01idle, waking: p01waking, going_back: p01going } },
  { id: "p02", name: "Miso", portrait: p02, hopBleed: 0.2812, clips: { delegating: p02delegating, working: p02working, delivered: p02delivered, idle: p02idle, waking: p02waking, going_back: p02going } },
  { id: "p03", name: "Pip", portrait: p03, hopBleed: 0.2812, clips: { delegating: p03delegating, working: p03working, delivered: p03delivered, idle: p03idle, waking: p03waking, going_back: p03going } },
  { id: "p04", name: "Nori", portrait: p04, hopBleed: 0.2812, clips: { delegating: p04delegating, working: p04working, delivered: p04delivered, idle: p04idle, waking: p04waking, going_back: p04going } },
  { id: "p06", name: "Kiko", portrait: p06, hopBleed: 0.2812, clips: { delegating: p06delegating, working: p06working, delivered: p06delivered, idle: p06idle, waking: p06waking, going_back: p06going } },
  { id: "p07", name: "Lumi", portrait: p07, hopBleed: 0.4375, clips: { delegating: p07delegating, working: p07working, delivered: p07delivered, idle: p07idle, waking: p07waking, going_back: p07going } },
  { id: "p08", name: "Poko", portrait: p08, hopBleed: 0.2812, clips: { delegating: p08delegating, working: p08working, delivered: p08delivered, idle: p08idle, waking: p08waking, going_back: p08going } },
  { id: "p09", name: "Tama", portrait: p09, hopBleed: 0.2812, clips: { delegating: p09delegating, working: p09working, delivered: p09delivered, idle: p09idle, waking: p09waking, going_back: p09going } },
  { id: "p10", name: "Sumi", portrait: p10, hopBleed: 0.2812, clips: { delegating: p10delegating, working: p10working, delivered: p10delivered, idle: p10idle, waking: p10waking, going_back: p10going } },
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
