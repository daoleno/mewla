import { describe, expect, test } from "bun:test";
import type { PaneScrollMode } from "./history";
import { clickBytes, mouseReport, paneForDrag, wheelBytes, wheelKind } from "./wheel";

const pane = (over: Partial<PaneScrollMode> = {}): PaneScrollMode => ({ id: "p", left: 0, top: 0, cols: 54, rows: 40, alternate: true, mouse: false, ...over });

describe("a drag on the alternate screen", () => {
  test("goes where the program can take it: mouse reports, else the wheel's cursor keys, else nowhere", () => {
    expect(wheelKind(pane({ mouse: true }))).toBe("mouse");
    expect(wheelKind(pane({ mouse: true, alt_scroll: true }))).toBe("mouse");
    expect(wheelKind(pane({ alt_scroll: true }))).toBe("keys");
    expect(wheelKind(pane())).toBe("none");
    expect(wheelKind(pane({ alt_scroll: false }))).toBe("none");
    // A shell's own screen has history the view scrolls; nothing is sent for it.
    expect(wheelKind(pane({ alternate: false, mouse: true, alt_scroll: true }))).toBe("none");
    expect(wheelKind(undefined)).toBe("none");
  });

  test("Claude Code's fullscreen view (mouse reports) gets one SGR wheel press per line at the pane's middle", () => {
    const p = pane({ mouse: true });
    // A finger moving down (lines > 0) is back in time: wheel up (64); the middle of 54 x 40 is column 28, row 21.
    expect(wheelBytes(p, 1)).toBe("\u001b[<64;28;21M");
    expect(wheelBytes(p, 3)).toBe("\u001b[<64;28;21M".repeat(3));
    // Back toward now: wheel down (65).
    expect(wheelBytes(p, -2)).toBe("\u001b[<65;28;21M".repeat(2));
  });

  test("a pane that is not at the origin is pressed in its own middle", () => {
    expect(wheelBytes(pane({ mouse: true, left: 41, top: 10, cols: 40, rows: 20 }), 1)).toBe("\u001b[<64;62;21M");
  });

  test("Codex's transcript (the wheel sends cursor keys) gets one Up or Down per line", () => {
    expect(wheelBytes(pane({ alt_scroll: true }), 2)).toBe("\u001b[A\u001b[A");
    expect(wheelBytes(pane({ alt_scroll: true }), -1)).toBe("\u001b[B");
    // Application cursor mode (DECCKM) uses SS3.
    expect(wheelBytes(pane({ alt_scroll: true, app_cursor: true }), 1)).toBe("\u001bOA");
  });

  test("a fling's flood is bounded and a program with nothing to scroll gets nothing", () => {
    expect(wheelBytes(pane({ mouse: true }), 300).length).toBe("\u001b[<64;28;21M".length * 30);
    expect(wheelBytes(pane({ alt_scroll: true }), -300).length).toBe(3 * 30);
    expect(wheelBytes(pane(), 5)).toBe("");
    expect(wheelBytes(pane({ mouse: true }), 0)).toBe("");
    expect(wheelBytes(undefined, 3)).toBe("");
  });

  test("the pane taking the drag is the one a wheel reaches", () => {
    const shell = pane({ id: "a", alternate: false });
    const agent = pane({ id: "b", left: 41, mouse: true });
    expect(paneForDrag([shell, agent])?.id).toBe("b");
    expect(paneForDrag([shell])?.id).toBe("a");
    expect(paneForDrag([])).toBeUndefined();
  });

  test("a wheel press goes in the encoding the program asked for", () => {
    expect(wheelBytes(pane({ mouse: true, mouse_format: "x10" }), 1)).toBe("\u001b[M`<5");
    expect(wheelBytes(pane({ mouse: true, mouse_format: "urxvt" }), -1)).toBe("\u001b[97;28;21M");
  });
});

describe("a tap on a program that takes mouse reports", () => {
  test("is a left click, press and release, at the cell (1-based on the wire)", () => {
    expect(clickBytes(pane({ mouse: true }), 9, 30)).toBe("\u001b[<0;10;31M\u001b[<0;10;31m");
    expect(clickBytes(pane({ mouse: true, mouse_format: "sgr" }), 0, 0)).toBe("\u001b[<0;1;1M\u001b[<0;1;1m");
  });

  test("in the older encodings: X10 and UTF-8 (button and position as characters), urxvt (decimal)", () => {
    // X10: ESC [ M, then 32 + button (release is button 3), 32 + column, 32 + row.
    expect(clickBytes(pane({ mouse: true, mouse_format: "x10" }), 9, 30)).toBe("\u001b[M *?\u001b[M#*?");
    expect(clickBytes(pane({ mouse: true, mouse_format: "utf8" }), 9, 30)).toBe("\u001b[M *?\u001b[M#*?");
    expect(clickBytes(pane({ mouse: true, mouse_format: "urxvt" }), 9, 30)).toBe("\u001b[32;10;31M\u001b[35;10;31M");
    // X10 cannot carry a cell past 95 as text; UTF-8 can.
    expect(clickBytes(pane({ mouse: true, mouse_format: "x10" }), 120, 3)).toBe("");
    expect(clickBytes(pane({ mouse: true, mouse_format: "utf8" }), 120, 3)).toBe("\u001b[M \u0099$\u001b[M#\u0099$");
    expect(mouseReport("sgr", 0, 5, 6, true)).toBe("\u001b[<0;5;6m");
  });

  test("is nothing for a program without mouse reports, or off the grid", () => {
    expect(clickBytes(pane(), 9, 30)).toBe("");
    expect(clickBytes(undefined, 9, 30)).toBe("");
    expect(clickBytes(pane({ mouse: true }), -1, 3)).toBe("");
  });
});
