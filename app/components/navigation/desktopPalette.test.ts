import { describe, expect, test } from "bun:test";
import { buildPaletteItems, filterPaletteItems, movePaletteSelection } from "./desktopPalette";

const items = buildPaletteItems({
  sessions: [
    { id: "%12", name: "atlas-notes", detail: "~/work/atlas" },
    { id: "%13", name: "calendar-sync", detail: "~/work/cal" },
  ],
  work: [{ workId: "w1", title: "Ship the release", statusLabel: "Needs you", sessionId: "%12" }],
});

describe("command palette", () => {
  test("holds actions, every place, Work and Sessions", () => {
    const ids = items.map((item) => item.id);
    expect(ids).toContain("action:new-session");
    expect(ids).toContain("go:settings");
    expect(ids).toContain("work:w1");
    expect(ids).toContain("session:%13");
    expect(items.find((item) => item.id === "go:calendar")?.shortcut).toBe("G then C");
    expect(items.find((item) => item.id === "work:w1")?.action).toEqual({ kind: "work", workId: "w1", sessionId: "%12" });
  });

  test("filters by every word and ranks label prefixes first", () => {
    const result = filterPaletteItems(items, "cal");
    expect(result[0]?.id).toBe("go:calendar");
    expect(result.map((item) => item.id)).toContain("session:%13");
    expect(filterPaletteItems(items, "atlas work").map((item) => item.id)).toEqual(["session:%12"]);
    expect(filterPaletteItems(items, "needs").map((item) => item.id)).toEqual(["work:w1"]);
    expect(filterPaletteItems(items, "zzz")).toEqual([]);
    expect(filterPaletteItems(items, "  ")).toHaveLength(items.length);
  });

  test("arrow keys wrap", () => {
    expect(movePaletteSelection(0, -1, 4)).toBe(3);
    expect(movePaletteSelection(3, 1, 4)).toBe(0);
    expect(movePaletteSelection(0, 1, 0)).toBe(0);
  });
});
