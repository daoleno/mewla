import { describe, expect, test } from "bun:test";
import { countsForm, countsWidth, countText, type CountPart } from "./counts";

const part = (kind: CountPart["kind"], n: number): CountPart => ({ kind, n, words: kind === "needs" ? (n === 1 ? "1 needs you" : `${n} need you`) : kind === "fresh" ? `${n} new` : `${n} running` });
// Text size and dot per surface: the Mac pane 13/7, the wide pane 17/8, the phone's tray 20/8.
const MAC = [13, 7] as const;
const PANE = [17, 8] as const;
const PHONE = [20, 8] as const;
const owner = [part("needs", 1), part("fresh", 1), part("running", 3)];
const busy = [part("needs", 12), part("fresh", 14), part("running", 17)];

describe("the tray's counts form", () => {
  test("the words stay while they fit, and an unmeasured line is the fullest", () => {
    expect(countsForm(owner, null, ...MAC)).toBe(0);
    expect(countsForm([part("running", 3)], 160, ...MAC)).toBe(0);
    expect(countsForm(owner, 600, ...PHONE)).toBe(0);
  });

  test("the owner's three counts on the Mac's narrowest column give way to dots and numbers", () => {
    // 240 less the pane's padding and its two 28 buttons.
    expect(countsForm(owner, 160, ...MAC)).toBe(2);
    expect(countsForm(owner, 250, ...MAC)).toBe(0);
  });

  test("needs you gives way before running does", () => {
    const room = (countsWidth(owner, 0, ...PHONE) + countsWidth(owner, 1, ...PHONE)) / 2;
    expect(countsForm(owner, room, ...PHONE)).toBe(1);
    expect(countText(owner[0]!, 1)).toBe("1");
    expect(countText(owner[2]!, 1)).toBe("3 running");
    expect(countText(owner[2]!, 2)).toBe("3");
    expect(countText(owner[1]!, 2)).toBe("1 new");
  });

  test("the shortest form of the busiest tray fits every column the app has", () => {
    // Room is the column less its padding and the two buttons: Mac 240, wide pane 280, phone 320 (the least).
    expect(countsWidth(busy, 2, ...MAC)).toBeLessThanOrEqual(240 - 20 - 56 - 4);
    expect(countsWidth(busy, 2, ...PANE)).toBeLessThanOrEqual(280 - 24 - 80 - 4);
    expect(countsWidth(busy, 2, ...PHONE)).toBeLessThanOrEqual(320 - 32 - 88 - 16);
  });
});
