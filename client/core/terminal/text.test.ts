import { describe, expect, test } from "bun:test";
import { retainedOverlap, TerminalHistoryModel, validateHistoryDelta, type HistoryDelta } from "./history";
import { findTerminalLink, linkAction, promptRows, promptTarget, rowText, searchCount, searchHistory } from "./text";

const delta = (patch: Partial<HistoryDelta>): HistoryDelta => ({
  pane_id: "mh-1", cols: 80, rows: 24, alternate: false, mouse: false, epoch: 1, reset: false, first: 0, from: 0, next: 0, lines: [], ...patch,
});

describe("rowText", () => {
  test("drops styling, OSC (hyperlinks, titles) and controls; keeps the text", () => {
    expect(rowText("\u001b[1;31mred\u001b[0m plain")).toBe("red plain");
    expect(rowText("\u001b]8;;https://x.dev\u001b\\link\u001b]8;;\u001b\\ done")).toBe("link done");
    expect(rowText("\u001b]0;title\u0007$ ls\r")).toBe("$ ls");
    expect(rowText("tab\there 中文")).toBe("tab\there 中文");
  });
});

describe("search", () => {
  const text = ["$ make", "error: one", "ok", "Error: two", "done"];

  test("a new query lands on the newest match; steps wrap both ways", () => {
    const first = searchHistory(text, "error", 0);
    expect(first).toEqual({ query: "error", hits: [1, 3], current: 3 });
    expect(searchCount(first)).toBe("2/2");
    const older = searchHistory(text, "error", -1, first);
    expect(older.current).toBe(1);
    expect(searchCount(older)).toBe("1/2");
    expect(searchHistory(text, "error", -1, older).current).toBe(3);
    expect(searchHistory(text, "error", 1, older).current).toBe(3);
  });

  test("no match counts 0; no query counts nothing", () => {
    expect(searchCount(searchHistory(text, "nope", 0))).toBe("0");
    expect(searchCount(searchHistory(text, "", 0))).toBe("");
  });
});

describe("prompts", () => {
  test("marked rows win over the shape of a prompt", () => {
    expect(promptRows(["$ ls", "x", "y"], [0, 4, 0])).toEqual([1]);
    expect(promptRows(["$ ls", "out", "me@box:~/src$ make", "❯ git status", "plain"], [0, 0, 0, 0, 0])).toEqual([0, 2, 3]);
  });

  test("previous is the last prompt above the top; next past the last is the bottom", () => {
    expect(promptTarget([2, 10, 30], 12, -1)).toBe(10);
    expect(promptTarget([2, 10, 30], 2, -1)).toBe(null);
    expect(promptTarget([2, 10, 30], 12, 1)).toBe(30);
    expect(promptTarget([2, 10, 30], 30, 1)).toBe("bottom");
  });
});

describe("links", () => {
  test("a URL under the tap, without trailing punctuation", () => {
    const line = "see https://mewla.dev/docs). next";
    expect(findTerminalLink(line, 6)).toEqual({ kind: "url", value: "https://mewla.dev/docs", start: 4, end: 26 });
    expect(findTerminalLink(line, 1)).toBe(null);
  });

  test("a path with its line and column; a file URL opens as a path", () => {
    expect(linkAction("  at src/app/main.ts:12:5", 6)).toEqual({ kind: "path", path: "src/app/main.ts", line: 12, column: 5 });
    expect(linkAction("open file:///home/me/a%20b.txt", 8)).toEqual({ kind: "path", path: "/home/me/a b.txt" });
    expect(linkAction("go https://x.dev/a now", 5)).toEqual({ kind: "url", url: "https://x.dev/a" });
    expect(linkAction("just words", 2)).toBe(null);
  });
});

describe("TerminalHistoryModel", () => {
  test("overlap: the held rows' suffix that starts the new snapshot", () => {
    expect(retainedOverlap(["a", "b", "c"], ["b", "c", "d"])).toBe(2);
    expect(retainedOverlap(["a", "b"], ["x"])).toBe(0);
    expect(retainedOverlap(["a", "a", "a"], ["a", "a", "b"])).toBe(2);
  });

  test("an unchanged snapshot is nothing to do; another pane starts over", () => {
    const model = new TerminalHistoryModel();
    const snap = { pane_id: "%1", cols: 80, total: 2, alternate: false, ansi: "a\nb\n" };
    expect(model.applySnapshot(snap)?.reset).toBe(true);
    expect(model.applySnapshot(snap)).toBe(null);
    expect(model.applySnapshot({ ...snap, pane_id: "%2" })).toEqual({ reset: true, removed: 0, rows: ["a", "b"], cols: 80, alternate: false });
    expect(model.replay()).toEqual({ reset: true, removed: 0, rows: ["a", "b"], cols: 80, alternate: false });
  });

  test("a delta with nothing new changes nothing; one that skips rows is a resync", () => {
    const model = new TerminalHistoryModel();
    model.applyDelta(delta({ reset: true, next: 2, lines: ["a", "b"] }));
    expect(model.applyDelta(delta({ from: 2, next: 2 }))).toBe(null);
    expect(model.applyDelta(delta({ from: 3, next: 4, lines: ["d"] }))).toBe("resync");
  });

  test("a delta outside its bounds is refused before the view allocates", () => {
    expect(() => validateHistoryDelta(delta({ from: 0, next: 5, lines: ["a"] }))).toThrow();
    expect(() => validateHistoryDelta(delta({ cols: 0 }))).toThrow();
    expect(() => validateHistoryDelta(delta({ next: 1, lines: ["a"], flags: [99] }))).toThrow();
    expect(validateHistoryDelta(delta({ next: 1, lines: ["a"], flags: [4] })).lines).toEqual(["a"]);
  });
});
