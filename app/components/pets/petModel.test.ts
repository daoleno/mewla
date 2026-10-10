import { describe, expect, test } from "bun:test";
import { readFileSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { BrainCatState } from "../mewla/brainCatState";
import { findPetPack, petInSeal, petTransition, type PetPack } from "./petModel";

const here = dirname(fileURLToPath(import.meta.url));
const STATES: BrainCatState[] = [
  "homeless", "offline", "waking", "idle", "working", "delegating", "attention", "delivered",
];

describe("pet transitions", () => {
  test("hops out of the seal on the way to its feet", () => {
    expect(petTransition("idle", "working")).toBe("waking");
    expect(petTransition("idle", "attention")).toBe("waking");
    expect(petTransition("offline", "delegating")).toBe("waking");
  });

  test("hops back in on the way down", () => {
    expect(petTransition("working", "idle")).toBe("going_back");
    expect(petTransition("attention", "idle")).toBe("going_back");
    // Connected after waking with nothing to do: back to bed.
    expect(petTransition("waking", "idle")).toBe("going_back");
  });

  test("cuts straight when it stays on one side, or the new clip is the hop", () => {
    expect(petTransition("working", "delegating")).toBeNull();
    expect(petTransition("delegating", "attention")).toBeNull();
    expect(petTransition("idle", "waking")).toBeNull();
    expect(petTransition("homeless", "working")).toBeNull();
    for (const state of STATES) expect(petTransition(state, state)).toBeNull();
  });

  test("the seal holds the sleeping, greyed and empty states", () => {
    expect(STATES.filter(petInSeal)).toEqual(["homeless", "offline", "idle"]);
  });
});

describe("pet catalog", () => {
  const catalog = readFileSync(join(here, "petPacks.ts"), "utf8");
  const ids = [...catalog.matchAll(/^    id: "([^"]+)",$/gm)].map((match) => match[1]);

  test("ships ten pets with p05 as the default", () => {
    expect(ids).toHaveLength(10);
    expect(new Set(ids).size).toBe(10);
    expect(catalog).toContain('export const DEFAULT_PET_ID = "p05";');
  });

  test("every pet has every clip and every required file exists", () => {
    for (const clip of [...STATES, "going_back"]) {
      expect(catalog.match(new RegExp(`^      ${clip}: \\{`, "gm"))).toHaveLength(ids.length);
    }
    for (const [, path] of catalog.matchAll(/require\("([^"]+)"\)/g)) {
      expect(existsSync(join(here, path))).toBe(true);
    }
  });

  test("an unknown or stale choice falls back to the default pet", () => {
    const packs = ["p01", "p05"].map((id) => ({ id }) as PetPack);
    expect(findPetPack(packs, "p01", "p05").id).toBe("p01");
    expect(findPetPack(packs, "gone", "p05").id).toBe("p05");
    expect(findPetPack(packs, null, "p05").id).toBe("p05");
  });
});
