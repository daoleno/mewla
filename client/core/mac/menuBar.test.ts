import { describe, expect, test } from "bun:test";
import { menuBarItem, menuBarReaction, peekOf, perchOrigin, stopWords } from "./menuBar";

describe("the menu bar cat", () => {
  test("away is hollow, whatever else waits", () => {
    expect(menuBarItem({ cat: "offline", needs: 2 })).toEqual({ image: "offline", needs: 0 });
    expect(menuBarItem({ cat: "homeless", needs: 0 }).image).toBe("offline");
  });
  test("needs you is the face with the count; Brain's turn turns the arc", () => {
    expect(menuBarItem({ cat: "attention", needs: 3 })).toEqual({ image: "needs", needs: 3 });
    expect(menuBarItem({ cat: "working", needs: 0 })).toEqual({ image: "arc", needs: 0 });
    expect(menuBarItem({ cat: "idle", needs: 0 })).toEqual({ image: "quiet", needs: 0 });
    expect(menuBarItem({ cat: "delegating", needs: 0 })).toEqual({ image: "quiet", needs: 0 });
    expect(menuBarItem({ cat: "waking", needs: 0 })).toEqual({ image: "quiet", needs: 0 });
  });
});

describe("the cat reacts once, never loops", () => {
  const at = (cat: Parameters<typeof menuBarItem>[0]["cat"], needs = 0) => ({ item: menuBarItem({ cat, needs}), cat });
  test("something starting to need you arrives; more of it does not again", () => {
    expect(menuBarReaction(at("working"), at("attention", 1))).toBe("arrive");
    expect(menuBarReaction(at("attention", 1), at("attention", 2))).toBeNull();
  });
  test("a Work coming back done nods once", () => {
    expect(menuBarReaction(at("delegating"), at("delivered"))).toBe("nod");
    expect(menuBarReaction(at("delivered"), at("delivered"))).toBeNull();
  });
  test("nothing on the first state", () => {
    expect(menuBarReaction(null, at("attention", 1))).toBeNull();
  });
});

describe("the perch under the item", () => {
  const area = { x: 0, y: 24, width: 1512, height: 958 };
  test("the pet's centre under the item's, the panel 23 pt under the menu bar (the window 19 higher, for the pet)", () => {
    expect(perchOrigin({ x: 900, y: 0, width: 30, height: 24 }, area)).toEqual({ x: 885, y: 28 });
  });
  test("kept 8 pt inside the screen", () => {
    expect(perchOrigin({ x: 1490, y: 0, width: 22, height: 24 }, area)).toEqual({ x: 1124, y: 28 });
    expect(perchOrigin({ x: 0, y: 0, width: 22, height: 24 }, area)).toEqual({ x: 8, y: 28 });
  });
});

describe("Stop Mewla on this Mac", () => {
  test("says what stops", () => {
    expect(stopWords(9)).toBe("9 running Sessions stop and phones can't reach this Mac until Mewla starts again.");
    expect(stopWords(1)).toBe("1 running Session stops and phones can't reach this Mac until Mewla starts again.");
    expect(stopWords(0)).toBe("Phones can't reach this Mac until Mewla starts again.");
  });
});

describe("the peek", () => {
  const need = (key: string, source: string, title: string, ask?: string) => ({ key, source, title, ask });
  test("who asks and what, once per request", () => {
    expect(peekOf(new Set(), [need("a", "DpIndexPause", "Run git push?", "Push tag v2026.11.20.3")])).toEqual({ key: "a", words: "DpIndexPause · Push tag v2026.11.20.3" });
    expect(peekOf(new Set(["a"]), [need("a", "DpIndexPause", "Run git push?")])).toBeNull();
  });
  test("cut to about 300 pt with an ellipsis", () => {
    const words = peekOf(new Set(), [need("b", "ResearchAnalytics", "looper#109 已合并。Perpetuo 现在就打开 pending_since，还是等周一发版？")])!.words;
    expect(words.length).toBe(48);
    expect(words.endsWith("…")).toBe(true);
  });
});
