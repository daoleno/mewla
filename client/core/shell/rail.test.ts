import { describe, expect, test } from "bun:test";
import { RAIL, RAIL_PLACES, railEdge, railLayout, railRoom } from "./rail";

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
    const room = railRoom(678, { top: 0, bottom: 20 });
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
  test("the system's items at the top of the bar are never the rail's", () => {
    expect(railRoom(466, { top: 0, bottom: 20 })).toBe(466 - RAIL.systemTop - 20 - RAIL.foot);
    expect(railRoom(100, { top: 0, bottom: 0 })).toBe(0);
  });
});
