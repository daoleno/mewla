import { describe, expect, test } from "bun:test";
import { MESSAGE_TABLE_CELL_LIMITS, messageTableColumnWidths } from "./InterfaceMessageTableModel";

describe("messageTableColumnWidths", () => {
  test("every row shares one width per column, sized by the widest cell", () => {
    const widths = messageTableColumnWidths(
      ["Area", "Light"],
      [["Editor background", "#ffffff"], ["Code", "Light syntax theme"]],
    );
    expect(widths).toHaveLength(2);
    expect(widths[0]).toBeGreaterThan(messageTableColumnWidths(["Area"], [["Code"]])[0]);
    expect(widths[1]).toBeGreaterThan(messageTableColumnWidths(["Light"], [["#ffffff"]])[0]);
  });

  test("short columns keep the minimum and long ones wrap at the maximum", () => {
    const [short, long] = messageTableColumnWidths(["#", "Notes"], [["1", "x".repeat(400)]]);
    expect(short).toBe(MESSAGE_TABLE_CELL_LIMITS.regular.min);
    expect(long).toBe(MESSAGE_TABLE_CELL_LIMITS.regular.max);
  });

  test("markdown syntax doesn't count and wide characters count double", () => {
    expect(messageTableColumnWidths(["**Area**"], [])).toEqual(messageTableColumnWidths(["Area"], []));
    expect(messageTableColumnWidths(["[docs](https://example.com/a/very/long/path)"], [])).toEqual(
      messageTableColumnWidths(["docs"], []),
    );
    const [latin] = messageTableColumnWidths(["abcdefghijkl"], []);
    const [cjk] = messageTableColumnWidths(["深色模式编辑器背景颜色对比"], []);
    expect(cjk).toBeGreaterThan(latin);
  });

  test("ragged rows still get a width for every column", () => {
    expect(messageTableColumnWidths(["A"], [["1", "2", "3"]], true)).toHaveLength(3);
  });
});
