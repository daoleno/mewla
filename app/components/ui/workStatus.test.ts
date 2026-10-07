import { describe, expect, test } from "bun:test";
import { resolveTheme } from "../../theme/resolve";
import {
  WORK_STATUS_GLYPHS,
  WORK_STATUS_LABELS,
  workStatusInk,
  type WorkStatus,
} from "./workStatus";

const STATES = Object.keys(WORK_STATUS_GLYPHS) as WorkStatus[];

describe("Seal & Slip Work states", () => {
  test("every state has its own glyph shape, so greyscale keeps them apart", () => {
    expect(STATES).toHaveLength(6);
    expect(new Set(Object.values(WORK_STATUS_GLYPHS)).size).toBe(STATES.length);
    expect(new Set(Object.values(WORK_STATUS_LABELS)).size).toBe(STATES.length);
  });

  test("only Needs you is the seal; failure is never vermilion", () => {
    for (const colorScheme of ["light", "dark"] as const) {
      const { colors } = resolveTheme({ colorScheme });
      const inks = {
        statusReady: colors.statusDone,
        statusRunning: colors.statusRunning,
        seal: colors.seal,
        sealText: colors.sealText,
        statusWarning: colors.statusWarning,
        statusFailed: colors.statusFailed,
        statusBlocked: colors.statusBlocked,
      };
      const sealed = STATES.filter((state) => workStatusInk(state, inks) === colors.seal);
      expect(sealed).toEqual(["needs"]);
    }
  });
});
