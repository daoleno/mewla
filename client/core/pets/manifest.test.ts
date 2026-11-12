import { describe, expect, test } from "bun:test";
import { existsSync, readdirSync } from "node:fs";
import { clipFrame, DEFAULT_PET_ID, PET_CLIPS, PET_MANIFEST, PET_STILLS, petEntry, resolvePetId } from "./manifest";

const ASSETS = `${import.meta.dir}/../../ui/assets/pets`;

describe("the pets", () => {
  test("six, Dango first and the default", () => {
    expect(PET_MANIFEST.map((pet) => `${pet.id} ${pet.name}`)).toEqual(["p05 Dango", "p11 Ash", "p12 Kami", "p13 Bit", "p14 Doodle", "p15 Riso"]);
    expect(DEFAULT_PET_ID).toBe("p05");
  });

  test("every pet ships every clip, still, its portrait and the empty bed, and nothing else is in the folder", () => {
    for (const pet of PET_MANIFEST) {
      const files = [...PET_CLIPS.map((clip) => `${clip}.webp`), ...PET_STILLS.map((still) => `${still}-still.webp`), "portrait.webp", "homeless.webp"];
      for (const file of files) expect(existsSync(`${ASSETS}/${pet.id}/${file}`)).toBe(true);
      expect(readdirSync(`${ASSETS}/${pet.id}`).sort()).toEqual(files.sort());
    }
    expect(readdirSync(ASSETS).sort()).toEqual(PET_MANIFEST.map((pet) => pet.id).sort());
  });

  test("a clip's length is its frames' delays; the hops, attention and delivered draw on the bleed canvas", () => {
    for (const pet of PET_MANIFEST) {
      for (const clip of PET_CLIPS) {
        const spec = pet.clips[clip];
        expect(spec.delays_ms.reduce((a, b) => a + b, 0)).toBe(spec.total_ms);
        const bleeds = clip === "waking" || clip === "going_back" || clip === "attention" || clip === "delivered";
        expect(spec.bleed).toBe(bleeds ? pet.hopBleed : 0);
        expect(spec.size).toBe(Math.round(pet.frame * (1 + 2 * spec.bleed)));
      }
    }
  });

  test("a removed pet falls back to Dango; the six stay chosen", () => {
    for (const removed of ["p01", "p03", "p09", "p10", "", null, undefined]) expect(resolvePetId(removed)).toBe("p05");
    expect(resolvePetId("p13")).toBe("p13");
    expect(petEntry("p02")).toBeUndefined();
  });

  test("a bleed clip is drawn larger than the box and centred on it, so the cat keeps its size", () => {
    expect(clipFrame({ bleed: 0 }, 40)).toEqual({ left: 0, top: 0, width: 40, height: 40 });
    const frame = clipFrame({ bleed: 0.25 }, 40);
    expect(frame).toEqual({ left: -10, top: -10, width: 60, height: 60 });
  });
});
