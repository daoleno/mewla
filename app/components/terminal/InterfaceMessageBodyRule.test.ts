import { describe, expect, test } from "bun:test";
import { inlinePartsText } from "../markdown/markdownInline";
import { parseMessageMarkdown } from "./InterfaceMessageMarkdownModel";

describe("Message body horizontal rules", () => {
  test("splits blocks at ---, *** and ___ but keeps table separators and list items", () => {
    const blocks = parseMessageMarkdown("one\n\n---\n\ntwo\n***\nthree\n___");
    expect(
      blocks.map((block) => (block.type === "paragraph" ? inlinePartsText(block.inline) : block.type)),
    ).toEqual(["one", "rule", "two", "rule", "three", "rule"]);
    expect(parseMessageMarkdown("| a |\n| --- |\n| b |")[0].type).toBe("table");
    expect(parseMessageMarkdown("- x\n- y")[0].type).toBe("list");
  });

  test("a --- right under a paragraph line is that paragraph's heading, as CommonMark says", () => {
    expect(parseMessageMarkdown("title\n---")).toMatchObject([{ type: "heading", level: 2 }]);
  });
});
