import { describe, expect, test } from "bun:test";
import { buildPaletteItems, filterPaletteItems } from "./palette";

const items = buildPaletteItems({
  sessions: [{ id: "w1", name: "api server", detail: "~/code/api" }, { id: "w2", name: "Calendar sync", detail: "~/code/cal" }],
  work: [{ workId: "k1", title: "Ship the release", statusLabel: "Needs you", sessionId: "w1" }],
});

describe("command palette", () => {
  test("actions, then places, Work and Sessions", () => {
    expect(items.map((item) => item.section)).toEqual([
      "Actions", "Actions", "Actions", "Actions", "Actions", "Actions",
      "Go to", "Go to", "Go to", "Go to", "Go to", "Go to", "Go to", "Go to", "Go to",
      "Work", "Sessions", "Sessions",
    ]);
    expect(items.slice(0, 6).map((item) => item.label)).toEqual(["New session", "New chat", "Open Brain's terminal", "Browse workspace", "Switch executor", "Keyboard shortcuts"]);
    expect(items.filter((item) => item.section === "Go to").map((item) => item.label)).toEqual(
      ["Brain", "All Sessions", "Calendar", "Computer", "Screen", "Extensions", "Stats", "Resources", "Settings"],
    );
    expect(items.find((item) => item.id === "go:calendar")?.shortcut).toBe("G then C");
    expect(items.find((item) => item.id === "work:k1")?.action).toEqual({ kind: "work", workId: "k1", sessionId: "w1" });
    expect(items.find((item) => item.id === "session:w2")?.action).toEqual({ kind: "session", workerId: "w2" });
  });

  test("an empty query keeps everything in order", () => {
    expect(filterPaletteItems(items, "  ")).toEqual(items);
  });

  test("every word must match; label prefixes rank first, then label matches, then details", () => {
    expect(filterPaletteItems(items, "cal").map((item) => item.label)).toEqual(["Calendar", "Calendar sync"]);
    expect(filterPaletteItems(items, "sync cal").map((item) => item.label)).toEqual(["Calendar sync"]);
    // `api` is a Session's label prefix and nowhere else.
    expect(filterPaletteItems(items, "api").map((item) => item.id)).toEqual(["session:w1"]);
    // `code` is only in details.
    expect(filterPaletteItems(items, "code").map((item) => item.id)).toEqual(["session:w1", "session:w2"]);
    // The section counts: "work" finds the Work row by its section.
    expect(filterPaletteItems(items, "needs").map((item) => item.id)).toEqual(["work:k1"]);
    expect(filterPaletteItems(items, "nothing here")).toEqual([]);
  });

  test("case and width do not matter", () => {
    expect(filterPaletteItems(items, "ＳＴＡＴＳ").map((item) => item.id)).toEqual(["go:stats"]);
  });
});
