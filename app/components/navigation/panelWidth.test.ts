import { describe, expect, test } from "bun:test";
import { clampPanelWidth } from "./panelWidth";

describe("panel width", () => {
  test("stored and dragged widths stay in range", () => {
    expect(clampPanelWidth(100, 216, 400)).toBe(216);
    expect(clampPanelWidth(999, 216, 400)).toBe(400);
    expect(clampPanelWidth(300.6, 216, 400)).toBe(301);
    expect(clampPanelWidth(Number.NaN, 216, 400)).toBe(216);
  });
});
