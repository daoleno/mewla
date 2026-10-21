import { expect, test } from "bun:test";
import { MENU_GAP, MENU_MARGIN, placeMessageMenu } from "./messageMenuPlacement";

const bounds = { top: 100, bottom: 800, left: 0, right: 400 };
const menu = { width: 220, height: 160 };

function place(anchor: { x: number; y: number; width: number; height: number }, touchY = anchor.y + 10, align: "start" | "end" = "end") {
  return placeMessageMenu({ anchor, touchY, menu, bounds, align });
}

test("opens below a message near the top, right-aligned to its edge", () => {
  const p = place({ x: 200, y: 150, width: 180, height: 44 });
  expect(p.side).toBe("below");
  expect(p.top).toBe(150 + 44 + MENU_GAP);
  expect(p.left).toBe(380 - menu.width);
  expect(p.origin).toEqual({ x: menu.width, y: 0 });
});

test("flips above a message near the bottom", () => {
  const p = place({ x: 200, y: 700, width: 180, height: 44 });
  expect(p.side).toBe("above");
  expect(p.top).toBe(700 - MENU_GAP - menu.height);
  expect(p.origin.y).toBe(menu.height);
});

test("a message taller than the screen anchors at the finger", () => {
  const p = place({ x: 20, y: -400, width: 360, height: 1600 }, 450);
  expect(p.side).toBe("touch");
  expect(p.top).toBe(450 + MENU_GAP);
});

test("a tall message pressed near the bottom opens above the finger", () => {
  const p = place({ x: 20, y: -400, width: 360, height: 1600 }, 780);
  expect(p.side).toBe("touch");
  expect(p.top).toBe(780 - MENU_GAP - menu.height);
});

test("a message half off the top still anchors where it can be seen", () => {
  const p = place({ x: 20, y: 20, width: 360, height: 90 }, 105);
  expect(p.side).toBe("below");
  expect(p.top).toBe(110 + MENU_GAP);
});

test("a message that is not on screen at all falls back to the finger", () => {
  const p = place({ x: 20, y: 900, width: 360, height: 50 }, 790);
  expect(p.side).toBe("touch");
});

test("the keyboard shortens the bounds and the menu flips above", () => {
  const p = placeMessageMenu({
    anchor: { x: 200, y: 400, width: 180, height: 44 },
    touchY: 410,
    menu,
    bounds: { ...bounds, bottom: 560 },
    align: "end",
  });
  expect(p.side).toBe("above");
});

test("start alignment follows the left edge and stays on screen", () => {
  const p = place({ x: 4, y: 300, width: 36, height: 32 }, 310, "start");
  expect(p.left).toBe(MENU_MARGIN);
  const right = place({ x: 380, y: 300, width: 30, height: 32 }, 310, "start");
  expect(right.left).toBe(bounds.right - MENU_MARGIN - menu.width);
});
