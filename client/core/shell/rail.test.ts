import { describe, expect, test } from "bun:test";
import { RAIL, RAIL_PLACES, railEdge, railEnds, railLayout, railPageOrder, railRoom } from "./rail";

const places = RAIL_PLACES.map((place) => place.id);

describe("railEdge", () => {
  test("the iPhone Duo's 84 pt bar on one side is the rail's edge, the left app in Split View has it on its left", () => {
    expect(railEdge({ os: "ios", phone: true, safe: { left: 0, right: 84 } })).toBe("right");
    expect(railEdge({ os: "ios", phone: true, safe: { left: 84, right: 0 } })).toBe("left");
  });
  test("never a phone's cutout on its side, Android, the web, a tablet, or upright", () => {
    expect(railEdge({ os: "ios", phone: true, safe: { left: 62, right: 62 } })).toBeNull();
    expect(railEdge({ os: "ios", phone: true, safe: { left: 0, right: 62 } })).toBeNull();
    expect(railEdge({ os: "ios", phone: true, safe: { left: 0, right: 0 } })).toBeNull();
    expect(railEdge({ os: "android", phone: true, safe: { left: 0, right: 84 } })).toBeNull();
    expect(railEdge({ os: "web", phone: true, safe: { left: 0, right: 84 } })).toBeNull();
    expect(railEdge({ os: "ios", phone: false, safe: { left: 0, right: 84 } })).toBeNull();
  });
});

describe("railLayout", () => {
  test("the Duo's cover upright (678 high) holds a Session's controls and every place", () => {
    const room = railRoom(678, railEnds({ width: 466, height: 678 }, { top: 0, bottom: 34 }, "right"));
    const layout = railLayout(["back", "stop", "git", "mode", "menu"], places, room);
    expect(layout).toEqual({ top: ["back", "stop", "git", "mode", "menu"], bottom: places, more: [] });
  });
  test("a short bar: the last place goes first into More, then the next; the page's controls stay on top", () => {
    const page = ["back", "git", "mode", "menu"];
    // Room for 7 buttons and the gap: 4 controls, 2 places and More.
    const room = 7 * RAIL.target + RAIL.groupGap;
    expect(railLayout(page, places, room)).toEqual({ top: page, bottom: ["brain", "state"], more: ["search", "screen", "new"] });
  });
  test("so short that the page's own controls overflow too: More holds the rest in order", () => {
    expect(railLayout(["back", "git", "mode", "menu"], places, 3 * RAIL.target + RAIL.groupGap)).toEqual({ top: ["back", "git"], bottom: [], more: ["mode", "menu", ...places] });
  });
  test("no page controls: the places alone, with no gap to keep", () => {
    expect(railLayout([], places, 5 * RAIL.target)).toEqual({ top: [], bottom: places, more: [] });
  });
  test("the system's items at the camera's end of the bar are never the rail's (the Duo simulator's measurements)", () => {
    const safe = { top: 0, bottom: 34 };
    // Cover upright: the status items reach y 174.
    expect(railEnds({ width: 466, height: 678 }, safe, "right")).toEqual({ top: RAIL.systemTopUpright, bottom: 34 + RAIL.foot });
    expect(RAIL.systemTopUpright).toBeGreaterThanOrEqual(174);
    // Cover on its side: the top is free, the camera's 82 at the foot (from y 384).
    expect(railEnds({ width: 678, height: 466 }, safe, "right")).toEqual({ top: RAIL.freeTop, bottom: RAIL.systemFootCover + RAIL.foot });
    expect(466 - RAIL.systemFootCover).toBeLessThanOrEqual(384);
    // Inner on its side: the top 124.
    expect(railEnds({ width: 951, height: 669 }, safe, "right")).toEqual({ top: RAIL.systemTopSide, bottom: 34 + RAIL.foot });
    expect(RAIL.systemTopSide).toBeGreaterThanOrEqual(124);
    expect(railRoom(100, { top: 80, bottom: 30 })).toBe(0);
  });
  test("a bar on the left was never measured: the system's reserve stays clear at both ends", () => {
    const safe = { top: 0, bottom: 34 };
    // The cover screen on its side the other way: its camera's end may be the top.
    const cover = railEnds({ width: 678, height: 466 }, safe, "left");
    expect(cover.top).toBeGreaterThanOrEqual(RAIL.systemFootCover);
    expect(cover.bottom).toBe(RAIL.systemFootCover + RAIL.foot);
    // Still Brain's ⋯ and the five places: 466 − 90 − 90 holds 6.
    expect(railLayout(["menu"], places, railRoom(466, cover)).more).toEqual([]);
    const inner = railEnds({ width: 951, height: 669 }, safe, "left");
    expect(inner).toEqual({ top: RAIL.systemTopSide, bottom: RAIL.systemTopSide + RAIL.foot });
  });
  test("the keyboard lifts the rail's foot above it, and what no longer fits goes into More", () => {
    const ends = railEnds({ width: 466, height: 678 }, { top: 0, bottom: 34 }, "right", 300);
    expect(ends.bottom).toBe(300 + RAIL.foot);
    const layout = railLayout(["back", "stop", "git", "mode", "menu"], places, railRoom(678, ends));
    expect(layout.more.length).toBeGreaterThan(0);
    // 678 − 180 − 308: 190 pt, three controls and More.
    expect(layout.top).toEqual(["back", "stop", "git"]);
  });
  test("the cover screen on its side holds a Session's controls with the places in More only when it must", () => {
    const room = railRoom(466, railEnds({ width: 678, height: 466 }, { top: 0, bottom: 34 }, "right"));
    expect(railLayout(["back", "git", "mode", "menu"], places, room)).toEqual({ top: ["back", "git", "mode", "menu"], bottom: ["brain", "state", "search"], more: ["screen", "new"] });
  });
});

describe("railPageOrder", () => {
  test("Back, then the steady controls, then those that come and go: Stop appearing moves nothing under a finger", () => {
    const back = [{ id: "back" }];
    const idle = [{ id: "menu" }];
    const running = [{ id: "stop", transient: true }, { id: "menu" }];
    expect(railPageOrder(back, idle).map((c) => c.id)).toEqual(["back", "menu"]);
    expect(railPageOrder(back, running).map((c) => c.id)).toEqual(["back", "menu", "stop"]);
    const session = [{ id: "stop", transient: true }, { id: "git" }, { id: "mode" }, { id: "menu" }];
    expect(railPageOrder([], session).map((c) => c.id)).toEqual(["git", "mode", "menu", "stop"]);
  });
});
