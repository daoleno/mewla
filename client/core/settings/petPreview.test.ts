import { describe, expect, test } from "bun:test";
import { existsSync } from "node:fs";
import { CLIP_TIMING, petInSeal, petTransition, PREVIEW_STATES, previewState } from "./petPreview";

describe("pet preview", () => {
  test("it runs through delegating, working, delivered and idle, then again", () => {
    expect([0, 1, 2, 3, 4].map(previewState)).toEqual(["delegating", "working", "delivered", "idle", "delegating"]);
  });

  test("leaving the seal hops out, going back hops in, the same side plays at once", () => {
    expect(petTransition("idle", "delegating")).toBe("waking");
    expect(petTransition("delivered", "idle")).toBe("going_back");
    expect(petTransition("delegating", "working")).toBeNull();
    expect(petTransition("homeless", "working")).toBeNull();
    expect(petTransition("idle", "waking")).toBeNull();
    expect(petInSeal("offline")).toBe(true);
  });

  test("every pet ships the clips the preview plays", () => {
    const dir = `${import.meta.dir}/../../ui/more/assets/pets`;
    const clips = [...new Set([...PREVIEW_STATES, "waking", "going_back"])] as (keyof typeof CLIP_TIMING)[];
    for (const pet of ["p01", "p02", "p03", "p04", "p05", "p06", "p07", "p08", "p09", "p10"]) {
      for (const clip of clips) expect(existsSync(`${dir}/${pet}/${clip}.webp`)).toBe(true);
    }
  });
});
