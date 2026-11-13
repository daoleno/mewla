import { describe, expect, test } from "bun:test";
import { sheetScrollHeight } from "./sheetFit";

describe("a sheet's scrolling part leaves room for what sits under it", () => {
  const chrome = 176;
  const fits = (window: number, keyboard: number) => {
    const scroll = sheetScrollHeight({ window, keyboard, chrome, cap: 520 });
    const lifted = keyboard > 0 ? keyboard + 16 : 0;
    return { scroll, bottom: scroll + chrome + lifted };
  };

  test("a tall phone keeps the full list", () => {
    expect(fits(874, 0).scroll).toBe(520);
  });

  test("short windows shrink the list so the sheet stays on screen (Duo inner 669, cover 466 on its side)", () => {
    for (const window of [669, 466, 402, 375]) {
      const { scroll, bottom } = fits(window, 0);
      expect(scroll).toBeLessThan(520);
      expect(bottom).toBeLessThanOrEqual(window);
    }
  });

  test("with the keyboard up the sheet still ends above it", () => {
    for (const [window, keyboard] of [[669, 264], [466, 230], [951, 336]]) {
      const { bottom } = fits(window, keyboard);
      expect(bottom).toBeLessThanOrEqual(window);
    }
  });

  test("never below the floor, never above the cap", () => {
    expect(sheetScrollHeight({ window: 300, keyboard: 250, chrome, cap: 520, floor: 40 })).toBe(40);
    expect(sheetScrollHeight({ window: 2000, keyboard: 0, chrome, cap: 520 })).toBe(520);
  });
});
