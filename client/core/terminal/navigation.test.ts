import { describe, expect, test } from "bun:test";
import { TerminalNavigator, type HistoryRows } from "./navigation";
import { searchCount } from "./text";

function navigator(rows: HistoryRows) {
  const calls: string[] = [];
  const held = { ...rows };
  const nav = new TerminalNavigator(() => held, {
    scrollTo: (row) => calls.push(`to:${row}`),
    scrollToBottom: () => calls.push("bottom"),
    mark: (row) => calls.push(`mark:${row}`),
  });
  nav.setGridRows(9);
  return { nav, calls, held };
}

const text = ["$ make", "cc main.c", "error: x", "$ make test", "ok", "error: y", "$ ls", "a b"];

describe("TerminalNavigator", () => {
  test("previous and next prompt from the row at the top; past the last prompt, the live screen", () => {
    const { nav, calls } = navigator({ text, flags: text.map(() => 0) });
    nav.viewport(text.length, true);
    nav.jumpPrompt(-1);
    nav.viewport(6, false);
    nav.jumpPrompt(-1);
    nav.viewport(3, false);
    nav.jumpPrompt(1);
    nav.viewport(6, false);
    nav.jumpPrompt(1);
    expect(calls).toEqual(["to:6", "to:3", "to:6", "bottom"]);
    expect(nav.scrolledUp).toBe(true);
  });

  test("marked prompts (OSC 133) are the ones it jumps between", () => {
    const { nav, calls } = navigator({ text, flags: [0, 4, 0, 0, 0, 0, 0, 0] });
    nav.viewport(5, false);
    nav.jumpPrompt(-1);
    expect(calls).toEqual(["to:1"]);
  });

  test("a search lands on the newest match a third down the view, and steps older and newer", () => {
    const { nav, calls } = navigator({ text, flags: [] });
    expect(searchCount(nav.runSearch("ERROR", 0))).toBe("2/2");
    expect(calls).toEqual(["mark:5", "to:2"]);
    expect(searchCount(nav.step(-1))).toBe("1/2");
    expect(calls.slice(2)).toEqual(["mark:2", "to:0"]);
    nav.clearSearch();
    expect(calls.at(-1)).toBe("mark:-1");
    expect(nav.step(1).query).toBe("");
  });

  test("the matches follow rows dropped from the front without moving the view", () => {
    const { nav, calls, held } = navigator({ text, flags: [] });
    nav.runSearch("error", 0);
    held.text = text.slice(2);
    nav.historyChanged({ reset: false, removed: 2, rows: [], cols: 80, alternate: false });
    expect(nav.search).toEqual({ query: "error", hits: [0, 3], current: 3 });
    expect(calls.filter((c) => c.startsWith("to:"))).toEqual(["to:2"]);
  });
});
