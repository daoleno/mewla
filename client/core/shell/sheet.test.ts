import { describe, expect, test } from "bun:test";
import { agentGridColumns, composing, DIALOG_FOOT, dialogBodyRoom, dialogFrame, SHEET, scrollBesidePinned, sheetBodyRoom, sheetFoot, sheetTitleGivesWay, taskCaptionShows } from "./sheet";

// The grabber (8 + 5) and the title row (11 + 25 + 19), as laid out; the title's room alone is 16.
const HEADER = 68;
const NO_TITLE = 29;
// New session under its scroll: the Task field with its caption (96), then Start (8 above a 52 pill).
const PINNED = 96 + 60;
// While composing: the focused field alone, Start in its row.
const COMPOSE_FIELD = 72;

describe("a sheet's body is held to what the window leaves (iPhone Duo, 11-14)", () => {
  test("a tall phone keeps its scroll near the 520 pt it had, the field and Start under it", () => {
    expect(scrollBesidePinned(sheetBodyRoom({ window: 874, keyboard: 0, header: HEADER }), PINNED)).toBe(513);
    expect(scrollBesidePinned(sheetBodyRoom({ window: 956, keyboard: 0, header: HEADER }), PINNED)).toBe(520);
  });

  test("the Duo's cover screen sideways (466 pt): the field and Start under a shorter scroll, all inside the sheet", () => {
    const room = sheetBodyRoom({ window: 466, keyboard: 0, header: HEADER });
    const scroll = scrollBesidePinned(room, PINNED);
    expect(scroll).toBeGreaterThan(100);
    expect(HEADER + scroll + PINNED + SHEET.foot).toBeLessThanOrEqual(Math.floor(466 * SHEET.maxShare));
  });

  test("the cover screen sideways with the keyboard up (1c): the field and Start fit over the keyboard with no scroll", () => {
    for (const keyboard of [200, 230, 260]) {
      expect(sheetTitleGivesWay(466, keyboard)).toBe(true);
      const room = sheetBodyRoom({ window: 466, keyboard, header: NO_TITLE });
      expect(room).toBeGreaterThanOrEqual(COMPOSE_FIELD);
    }
  });

  test("the keyboard up in a tall window keeps the title", () => {
    expect(sheetTitleGivesWay(874, 300)).toBe(false);
    expect(sheetTitleGivesWay(466, 0)).toBe(false);
  });

  test("never negative", () => {
    expect(sheetBodyRoom({ window: 200, keyboard: 300, header: HEADER })).toBe(0);
    expect(scrollBesidePinned(0, PINNED)).toBe(0);
  });
});

describe("New session while a field has the keyboard", () => {
  test("composes only with the keyboard up for one of its fields", () => {
    expect(composing(260, "task")).toBe(true);
    expect(composing(0, "task")).toBe(false);
    expect(composing(260, null)).toBe(false);
  });

  test("the Task caption gives its room to the agents on a short window and while composing", () => {
    expect(taskCaptionShows(874, false)).toBe(true);
    expect(taskCaptionShows(669, false)).toBe(true);
    expect(taskCaptionShows(466, false)).toBe(false);
    expect(taskCaptionShows(411, false)).toBe(false);
    expect(taskCaptionShows(874, true)).toBe(false);
  });

  test("its agents three across on a phone, more on a wide sheet so the folders show (1a)", () => {
    expect(agentGridColumns(358, 9)).toBe(3);
    expect(agentGridColumns(560, 9)).toBe(6);
    expect(agentGridColumns(835, 9)).toBe(9);
  });
});

describe("on wide a sheet is a dialog the keyboard never covers (iPhone Duo open, 11-14)", () => {
  // The dialog's title row (pt-3, then 11 + 25 + 19).
  const DIALOG_HEADER = 67;

  test("without a keyboard: 88 % of the window, centred", () => {
    expect(dialogFrame(669, 0)).toEqual({ lift: 0, max: 588 });
    expect(dialogFrame(800, 0)).toEqual({ lift: 0, max: 704 });
  });

  test("an Android foldable unfolded: the dialog and its body's room both leave out the safe areas, so Start keeps its foot", () => {
    // 7.6" foldable unfolded, landscape: 701 high, a 24 pt status bar and a 48 pt taskbar (Mac run 11-14: Start flush
    // on the dialog's bottom edge, clipped by its corner, as the room counted the bars the dialog's box left out).
    const frame = dialogFrame(701, 0, 24 + 48);
    expect(frame).toEqual({ lift: 0, max: Math.floor((701 - 72) * 0.88) });
    // Header, room and foot add up to the box AppSheet gives the dialog: nothing runs past its bottom.
    const room = dialogBodyRoom({ window: 701, keyboard: 0, header: DIALOG_HEADER, insets: 72 });
    expect(DIALOG_HEADER + room + DIALOG_FOOT).toBe(frame.max);
    // Start's foot is the 16 the dialog's content keeps at its sides.
    expect(DIALOG_FOOT).toBe(16);
  });

  test("with the keyboard up it sits in what the keyboard leaves, 16 clear of it and of the top", () => {
    // iPhone Duo inner screen on its side: 669 high, a 264 pt keyboard.
    expect(dialogFrame(669, 264)).toEqual({ lift: 280, max: 373 });
    expect(dialogFrame(100, 264).max).toBe(0);
  });

  test("New session in the dialog on the open Duo: a scroll beside the pinned field and Start; typing, the field and Start fit", () => {
    const flat = dialogBodyRoom({ window: 669, keyboard: 0, header: DIALOG_HEADER });
    expect(flat).toBe(588 - 67 - 16);
    expect(scrollBesidePinned(flat, PINNED)).toBe(505 - PINNED);
    const typing = dialogBodyRoom({ window: 669, keyboard: 264, header: DIALOG_HEADER });
    expect(typing).toBeGreaterThanOrEqual(COMPOSE_FIELD);
  });
});
