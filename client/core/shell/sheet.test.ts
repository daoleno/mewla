import { describe, expect, test } from "bun:test";
import { dialogBodyRoom, dialogFrame, SHEET, scrollAboveControl, sheetBodyRoom, sheetFoot, sheetTitleGivesWay } from "./sheet";

// The grabber (8 + 5) and the title row (11 + 25 + 19), as laid out; the title's room alone is 16.
const HEADER = 68;
const NO_TITLE = 29;
// New session's Start: 8 above a 52 pill.
const START = 60;
const NEW_SESSION = { max: 520, minScroll: 120 };

describe("a sheet's body is held to what the window leaves (iPhone Duo, 11-14)", () => {
  test("a tall phone keeps the 520 pt it always had", () => {
    const room = sheetBodyRoom({ window: 874, keyboard: 0, header: HEADER });
    expect(scrollAboveControl(room, START, NEW_SESSION)).toEqual({ scroll: 520, controlInside: false });
  });

  test("the Duo's inner screen sideways (669 pt): Start stays inside the sheet", () => {
    const room = sheetBodyRoom({ window: 669, keyboard: 0, header: HEADER });
    const { scroll, controlInside } = scrollAboveControl(room, START, NEW_SESSION);
    expect(controlInside).toBe(false);
    // The whole sheet fits its 88 % cap.
    expect(HEADER + scroll + START + SHEET.foot).toBeLessThanOrEqual(Math.floor(669 * SHEET.maxShare));
  });

  test("the outer screen sideways (466 pt)", () => {
    const room = sheetBodyRoom({ window: 466, keyboard: 0, header: HEADER });
    const { scroll, controlInside } = scrollAboveControl(room, START, NEW_SESSION);
    expect(controlInside).toBe(false);
    expect(HEADER + scroll + START + SHEET.foot).toBeLessThanOrEqual(Math.floor(466 * SHEET.maxShare));
  });

  test("the outer screen sideways with the keyboard up: the title gives way and Start goes to the scroll's end", () => {
    expect(sheetTitleGivesWay(466, 200)).toBe(true);
    const room = sheetBodyRoom({ window: 466, keyboard: 200, header: NO_TITLE });
    const { scroll, controlInside } = scrollAboveControl(room, START, NEW_SESSION);
    expect(controlInside).toBe(true);
    // The scroll still fits between the header and the keyboard, so scrolling reaches Start.
    expect(NO_TITLE + scroll + sheetFoot(200) + 200 + SHEET.keyboardGap).toBeLessThanOrEqual(Math.floor(466 * SHEET.maxShare));
    expect(scroll).toBeGreaterThan(START);
  });

  test("the keyboard up in a tall window keeps the title", () => {
    expect(sheetTitleGivesWay(874, 300)).toBe(false);
    expect(sheetTitleGivesWay(466, 0)).toBe(false);
  });

  test("never negative", () => {
    expect(sheetBodyRoom({ window: 200, keyboard: 300, header: HEADER })).toBe(0);
    expect(scrollAboveControl(0, START, NEW_SESSION)).toEqual({ scroll: 0, controlInside: true });
  });
});

describe("on wide a sheet is a dialog the keyboard never covers (iPhone Duo open, 11-14)", () => {
  // The dialog's title row (pt-3, then 11 + 25 + 19).
  const DIALOG_HEADER = 67;

  test("without a keyboard: 88 % of the window, centred", () => {
    expect(dialogFrame(669, 0)).toEqual({ lift: 0, max: 588 });
    expect(dialogFrame(800, 0)).toEqual({ lift: 0, max: 704 });
  });

  test("with the keyboard up it sits in what the keyboard leaves, 16 clear of it and of the top", () => {
    // iPhone Duo inner screen on its side: 669 high, a 264 pt keyboard.
    expect(dialogFrame(669, 264)).toEqual({ lift: 280, max: 373 });
    expect(dialogFrame(100, 264).max).toBe(0);
  });

  test("New session in the dialog keeps Start beside its scroll on the open Duo, at its end once the keyboard is up", () => {
    const flat = dialogBodyRoom({ window: 669, keyboard: 0, header: DIALOG_HEADER });
    expect(flat).toBe(588 - 67 - 24);
    expect(scrollAboveControl(flat, START, NEW_SESSION)).toEqual({ scroll: 437, controlInside: false });
    const typing = dialogBodyRoom({ window: 669, keyboard: 264, header: DIALOG_HEADER });
    expect(typing).toBe(373 - 67 - 24);
    expect(scrollAboveControl(typing, START, NEW_SESSION)).toEqual({ scroll: 222, controlInside: false });
  });
});
