import { describe, expect, test } from "bun:test";
import { inlinePartsText } from "../markdown/markdownInline";
import { parseMessageMarkdown } from "./InterfaceMessageMarkdownModel";

const texts = (value: string, keepLineBreaks = false) =>
  parseMessageMarkdown(value, { keepLineBreaks }).map((block) =>
    block.type === "paragraph" ? inlinePartsText(block.inline) : block.type,
  );

describe("message line breaks", () => {
  const typed = "first line\nsecond line\n\nnew paragraph";

  test("an agent's wrapped prose joins into one paragraph", () => {
    expect(texts(typed)).toEqual(["first line second line", "new paragraph"]);
  });

  test("a person's own message keeps the breaks they typed", () => {
    expect(texts(typed, true)).toEqual(["first line\nsecond line", "new paragraph"]);
  });

  test("a hard break (two spaces or a backslash) is a break for an agent too", () => {
    expect(texts("one  \ntwo\\\nthree")).toEqual(["one\ntwo\nthree"]);
  });

  test("a <br> is a break", () => {
    expect(texts("one<br>two")).toEqual(["one\ntwo"]);
  });
});
