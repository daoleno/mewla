import p01_idle from "../assets/pets/p01/idle.webp";
import p01_waking from "../assets/pets/p01/waking.webp";
import p02_idle from "../assets/pets/p02/idle.webp";
import p02_waking from "../assets/pets/p02/waking.webp";
import p03_idle from "../assets/pets/p03/idle.webp";
import p03_waking from "../assets/pets/p03/waking.webp";
import p04_idle from "../assets/pets/p04/idle.webp";
import p04_waking from "../assets/pets/p04/waking.webp";
import p05_idle from "../assets/pets/p05/idle.webp";
import p05_waking from "../assets/pets/p05/waking.webp";
import p06_idle from "../assets/pets/p06/idle.webp";
import p06_waking from "../assets/pets/p06/waking.webp";
import p07_idle from "../assets/pets/p07/idle.webp";
import p07_waking from "../assets/pets/p07/waking.webp";
import p08_idle from "../assets/pets/p08/idle.webp";
import p08_waking from "../assets/pets/p08/waking.webp";
import p09_idle from "../assets/pets/p09/idle.webp";
import p09_waking from "../assets/pets/p09/waking.webp";
import p10_idle from "../assets/pets/p10/idle.webp";
import p10_waking from "../assets/pets/p10/waking.webp";
import type { ImageAsset } from "nsh";
import { pet } from "../more/pet/pets";
import p01_attention from "../assets/pets/p01/attention.webp";
import p01_working from "../assets/pets/p01/working.webp";
import p01_delegating from "../assets/pets/p01/delegating.webp";
import p01_offline from "../assets/pets/p01/offline.webp";
import p01_homeless from "../assets/pets/p01/homeless.webp";
import p02_attention from "../assets/pets/p02/attention.webp";
import p02_working from "../assets/pets/p02/working.webp";
import p02_delegating from "../assets/pets/p02/delegating.webp";
import p02_offline from "../assets/pets/p02/offline.webp";
import p02_homeless from "../assets/pets/p02/homeless.webp";
import p03_attention from "../assets/pets/p03/attention.webp";
import p03_working from "../assets/pets/p03/working.webp";
import p03_delegating from "../assets/pets/p03/delegating.webp";
import p03_offline from "../assets/pets/p03/offline.webp";
import p03_homeless from "../assets/pets/p03/homeless.webp";
import p04_attention from "../assets/pets/p04/attention.webp";
import p04_working from "../assets/pets/p04/working.webp";
import p04_delegating from "../assets/pets/p04/delegating.webp";
import p04_offline from "../assets/pets/p04/offline.webp";
import p04_homeless from "../assets/pets/p04/homeless.webp";
import p05_attention from "../assets/pets/p05/attention.webp";
import p05_working from "../assets/pets/p05/working.webp";
import p05_delegating from "../assets/pets/p05/delegating.webp";
import p05_offline from "../assets/pets/p05/offline.webp";
import p05_homeless from "../assets/pets/p05/homeless.webp";
import p06_attention from "../assets/pets/p06/attention.webp";
import p06_working from "../assets/pets/p06/working.webp";
import p06_delegating from "../assets/pets/p06/delegating.webp";
import p06_offline from "../assets/pets/p06/offline.webp";
import p06_homeless from "../assets/pets/p06/homeless.webp";
import p07_attention from "../assets/pets/p07/attention.webp";
import p07_working from "../assets/pets/p07/working.webp";
import p07_delegating from "../assets/pets/p07/delegating.webp";
import p07_offline from "../assets/pets/p07/offline.webp";
import p07_homeless from "../assets/pets/p07/homeless.webp";
import p08_attention from "../assets/pets/p08/attention.webp";
import p08_working from "../assets/pets/p08/working.webp";
import p08_delegating from "../assets/pets/p08/delegating.webp";
import p08_offline from "../assets/pets/p08/offline.webp";
import p08_homeless from "../assets/pets/p08/homeless.webp";
import p09_attention from "../assets/pets/p09/attention.webp";
import p09_working from "../assets/pets/p09/working.webp";
import p09_delegating from "../assets/pets/p09/delegating.webp";
import p09_offline from "../assets/pets/p09/offline.webp";
import p09_homeless from "../assets/pets/p09/homeless.webp";
import p10_attention from "../assets/pets/p10/attention.webp";
import p10_working from "../assets/pets/p10/working.webp";
import p10_delegating from "../assets/pets/p10/delegating.webp";
import p10_offline from "../assets/pets/p10/offline.webp";
import p10_homeless from "../assets/pets/p10/homeless.webp";

/**
 * The pets' animated clips (from app/assets/pets: the default at 256 px, the rest at 192 px) for the
 * states the app shows so far; DESIGN.md's state map names the rest. Add a state here, for every pet.
 */
export type PetClip = "attention" | "working" | "delegating" | "offline" | "homeless" | "idle" | "waking";

const CLIPS: Record<string, Record<PetClip, ImageAsset>> = {
  p01: { attention: p01_attention, working: p01_working, delegating: p01_delegating, offline: p01_offline, homeless: p01_homeless, idle: p01_idle, waking: p01_waking },
  p02: { attention: p02_attention, working: p02_working, delegating: p02_delegating, offline: p02_offline, homeless: p02_homeless, idle: p02_idle, waking: p02_waking },
  p03: { attention: p03_attention, working: p03_working, delegating: p03_delegating, offline: p03_offline, homeless: p03_homeless, idle: p03_idle, waking: p03_waking },
  p04: { attention: p04_attention, working: p04_working, delegating: p04_delegating, offline: p04_offline, homeless: p04_homeless, idle: p04_idle, waking: p04_waking },
  p05: { attention: p05_attention, working: p05_working, delegating: p05_delegating, offline: p05_offline, homeless: p05_homeless, idle: p05_idle, waking: p05_waking },
  p06: { attention: p06_attention, working: p06_working, delegating: p06_delegating, offline: p06_offline, homeless: p06_homeless, idle: p06_idle, waking: p06_waking },
  p07: { attention: p07_attention, working: p07_working, delegating: p07_delegating, offline: p07_offline, homeless: p07_homeless, idle: p07_idle, waking: p07_waking },
  p08: { attention: p08_attention, working: p08_working, delegating: p08_delegating, offline: p08_offline, homeless: p08_homeless, idle: p08_idle, waking: p08_waking },
  p09: { attention: p09_attention, working: p09_working, delegating: p09_delegating, offline: p09_offline, homeless: p09_homeless, idle: p09_idle, waking: p09_waking },
  p10: { attention: p10_attention, working: p10_working, delegating: p10_delegating, offline: p10_offline, homeless: p10_homeless, idle: p10_idle, waking: p10_waking },
};

/** The chosen pet's clip (reactive: follows a change of pet). */
export function petClip(clip: PetClip): ImageAsset {
  return (CLIPS[pet().id] ?? CLIPS.p05)[clip];
}
