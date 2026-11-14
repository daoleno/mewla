import { describe, expect, test } from "bun:test";
import { petInSeal, petTransition } from "./petPreview";

describe("pet preview", () => {
  test("leaving the nap hops out, going back hops in, the same side plays at once", () => {
    expect(petTransition("idle", "delegating")).toBe("waking");
    expect(petTransition("delivered", "idle")).toBe("going_back");
    expect(petTransition("working", "offline")).toBe("going_back");
    expect(petTransition("delegating", "working")).toBeNull();
    expect(petTransition("homeless", "working")).toBeNull();
    expect(petTransition("working", "homeless")).toBeNull();
    expect(petTransition("idle", "waking")).toBeNull();
    expect(petInSeal("offline")).toBe(true);
  });
});
