import { describe, expect, test } from "bun:test";
import { BESIDE_MIN_WIDTH, intoBeside, opensBeside, outOfBeside, showsBeside, shownRoutes } from "./beside";

const home = { name: "home" };
const session = { name: "session", workerId: "w1" };
const work = { name: "workDetail", workId: "k1" };
const settings = { name: "settings" };

describe("beside", () => {
  test("splits a desktop or browser window from a 1480 right pane: a 1920 window, not a 1440 one, never a phone", () => {
    expect(showsBeside({ desktop: true, wide: true, rightPane: 1920 - 320 })).toBe(true);
    expect(showsBeside({ desktop: true, wide: true, rightPane: 1920 - 420 })).toBe(true);
    expect(showsBeside({ desktop: true, wide: true, rightPane: 1440 - 320 })).toBe(false);
    expect(showsBeside({ desktop: true, wide: true, rightPane: BESIDE_MIN_WIDTH - 1 })).toBe(false);
    expect(showsBeside({ desktop: false, wide: true, rightPane: 3000 })).toBe(false);
    expect(showsBeside({ desktop: true, wide: false, rightPane: 3000 })).toBe(false);
  });

  test("a Session, a Work and a Work file open beside; Settings and Calendar cover both", () => {
    expect([session, work, { name: "workFile", id: "f" }, { name: "files-preview" }].every(opensBeside)).toBe(true);
    expect([settings, { name: "calendar" }, { name: "computer" }, undefined].some(opensBeside)).toBe(false);
  });

  test("crossing in moves the pages above Brain beside it; crossing out puts them back on the stack", () => {
    expect(intoBeside([home, work, session])).toEqual({ main: [home], beside: [work, session] });
    expect(intoBeside([home])).toBeNull();
    expect(intoBeside([home, settings, session])).toBeNull();
    expect(outOfBeside([home], [work, session])).toEqual([home, work, session]);
    expect(outOfBeside([home], [])).toBeNull();
    expect(outOfBeside([home, settings], [work])).toBeNull();
  });

  test("what the URL reads: Brain and what stands beside it while Brain shows", () => {
    expect(shownRoutes([home], [session], true)).toEqual([home, session]);
    expect(shownRoutes([home, settings], [session], true)).toEqual([home, settings]);
    expect(shownRoutes([home], [session], false)).toEqual([home]);
  });
});
