import { describe, expect, test } from "bun:test";
import { parseMessageBlocks } from "./InterfaceMessageBodyModel";

describe("message line breaks", () => {
  const typed = "first line\nsecond line\n\nnew paragraph";

  test("an agent's wrapped prose joins into one paragraph", () => {
    expect(parseMessageBlocks(typed)).toEqual([
      { type: "paragraph", text: "first line second line" },
      { type: "paragraph", text: "new paragraph" },
    ]);
  });

  test("a person's own message keeps the breaks they typed", () => {
    expect(parseMessageBlocks(typed, { keepLineBreaks: true })).toEqual([
      { type: "paragraph", text: "first line\nsecond line" },
      { type: "paragraph", text: "new paragraph" },
    ]);
  });
});
