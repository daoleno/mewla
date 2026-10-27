import { describe, expect, test } from "bun:test";
import { DIALOG_INSET, dialogCardInset } from "./dialogCardInset";

describe("dialogCardInset", () => {
  test("a tight phone sheet opens up to the dialog inset", () => {
    expect(dialogCardInset({ paddingHorizontal: 14, paddingTop: 10, paddingBottom: 18 })).toEqual({
      paddingHorizontal: DIALOG_INSET,
      paddingTop: DIALOG_INSET,
      paddingBottom: DIALOG_INSET,
    });
  });

  test("the frame's own inset applies when a caller sets none", () => {
    expect(dialogCardInset({})).toEqual({
      paddingHorizontal: DIALOG_INSET,
      paddingTop: DIALOG_INSET,
      paddingBottom: DIALOG_INSET,
    });
  });

  test("full-bleed and roomier callers keep theirs", () => {
    expect(dialogCardInset({ paddingHorizontal: 0, paddingBottom: 0, paddingTop: 40 })).toEqual({
      paddingHorizontal: 0,
      paddingTop: 40,
      paddingBottom: 0,
    });
  });
});
