import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { PET_CLIPS, PET_MANIFEST, PET_STILLS } from "./manifest";

// The app's table of pet images (ui/pets/assets.ts) names one file per clip, still, portrait and empty
// bed of every pet in the manifest; scripts/check-pet-assets.py checks each platform's build holds them.
const UI = join(import.meta.dir, "..", "..", "ui");
const table = readFileSync(join(UI, "pets", "assets.ts"), "utf8");
const imported = new Set([...table.matchAll(/from "\.\.\/(assets\/pets\/[^"]+)"/g)].map((match) => match[1]!));

describe("pet images", () => {
  test("the table imports every image the manifest's pets need, and nothing else", () => {
    const needed = PET_MANIFEST.flatMap((pet) => [
      ...PET_CLIPS.map((clip) => `assets/pets/${pet.id}/${clip}.webp`),
      ...PET_STILLS.map((still) => `assets/pets/${pet.id}/${still}-still.webp`),
      `assets/pets/${pet.id}/portrait.webp`,
      `assets/pets/${pet.id}/homeless.webp`,
    ]);
    expect([...imported].sort()).toEqual([...new Set(needed)].sort());
  });

  test("every image the table imports is in the source tree", () => {
    expect([...imported].filter((path) => !existsSync(join(UI, path)))).toEqual([]);
  });
});
