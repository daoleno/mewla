// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { parseMessageBlocks } from "./InterfaceMessageBodyModel";

describe("Message body horizontal rules", () => {
  test("splits blocks at ---, *** and ___ but keeps table separators and list items", () => {
    expect(parseMessageBlocks("one\n\n---\n\ntwo\n***\nthree\n___")).toEqual([
      { type: "paragraph", text: "one" },
      { type: "rule" },
      { type: "paragraph", text: "two" },
      { type: "rule" },
      { type: "paragraph", text: "three" },
      { type: "rule" },
    ]);
    expect(parseMessageBlocks("| a |\n| --- |\n| b |")[0].type).toBe("table");
    expect(parseMessageBlocks("- x\n- y")[0].type).toBe("list");
  });
});
