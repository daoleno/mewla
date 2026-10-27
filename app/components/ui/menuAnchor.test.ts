import { describe, expect, test } from "bun:test";
import { MENU_POPOVER_WIDTH, menuPopoverPosition } from "./menuAnchor";

const viewport = { width: 1440, height: 900 };

describe("menuPopoverPosition", () => {
  test("a control on the right opens the menu below it, right edges aligned", () => {
    const anchor = { x: 1390, y: 6, width: 40, height: 40 };
    expect(menuPopoverPosition(anchor, 200, viewport)).toEqual({ left: 1430 - MENU_POPOVER_WIDTH, top: 52 });
  });

  test("a control on the left opens the menu from its left edge", () => {
    const anchor = { x: 300, y: 100, width: 40, height: 40 };
    expect(menuPopoverPosition(anchor, 200, viewport).left).toBe(300);
  });

  test("near the bottom the menu opens above the control", () => {
    const anchor = { x: 1376, y: 824, width: 48, height: 48 };
    expect(menuPopoverPosition(anchor, 200, viewport).top).toBe(824 - 6 - 200);
  });

  test("the menu stays inside the window", () => {
    const anchor = { x: 0, y: 0, width: 10, height: 10 };
    const tall = menuPopoverPosition(anchor, 2000, viewport);
    expect(tall.left).toBe(8);
    expect(tall.top).toBe(8);
  });
});
