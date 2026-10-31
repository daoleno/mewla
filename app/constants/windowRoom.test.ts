import { describe, expect, test } from "bun:test";
import { shouldDockSidebar, shouldShowBrainWorkColumn } from "./windowRoom";

describe("window room", () => {
  test("a foldable's inner screen docks in both orientations", () => {
    expect(shouldDockSidebar(951, 669)).toBe(true);
    expect(shouldDockSidebar(669, 951)).toBe(true);
  });

  test("phones dock only when they are not sideways or narrow", () => {
    expect(shouldDockSidebar(390, 844)).toBe(false);
    expect(shouldDockSidebar(932, 430)).toBe(false);
    expect(shouldDockSidebar(466, 678)).toBe(false);
    expect(shouldDockSidebar(678, 466)).toBe(false);
  });

  test("tablets and desktop windows dock", () => {
    expect(shouldDockSidebar(1024, 768)).toBe(true);
    expect(shouldDockSidebar(1440, 900)).toBe(true);
  });

  test("Brain's Work column needs the wide dock", () => {
    expect(shouldShowBrainWorkColumn(951, 669)).toBe(false);
    expect(shouldShowBrainWorkColumn(1024, 768)).toBe(true);
    expect(shouldShowBrainWorkColumn(1280, 400)).toBe(false);
  });
});
