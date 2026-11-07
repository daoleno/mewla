import { describe, expect, test } from "bun:test";
import { parseMessageMarkdown, type MessageBlock } from "./InterfaceMessageMarkdownModel";

function firstCode(blocks: MessageBlock[]): MessageBlock | undefined {
  for (const block of blocks) {
    if (block.type === "code") return block;
    if (block.type === "list") {
      for (const item of block.items) {
        const found = firstCode(item.blocks);
        if (found) return found;
      }
    }
    if (block.type === "quote") {
      const found = firstCode(block.blocks);
      if (found) return found;
    }
  }
  return undefined;
}

describe("Message body code fences", () => {
  test("a fence inside a list item drops the item's indent from its code", () => {
    const blocks = parseMessageMarkdown("1. Run it:\n\n   ```bash\n   cd app\n     bun test\n   ```");
    expect(firstCode(blocks)).toMatchObject({
      type: "code",
      text: "cd app\n  bun test",
      language: "bash",
      closed: true,
    });
  });

  test("code indented less than its fence keeps what it has", () => {
    expect(firstCode(parseMessageMarkdown("   ```\n x\n```"))).toMatchObject({ text: "x" });
  });

  test("the language is the first word of the info string, lowercased and without braces", () => {
    expect(firstCode(parseMessageMarkdown("```TS {1,3}\nx\n```"))).toMatchObject({ language: "ts" });
    expect(firstCode(parseMessageMarkdown("```language-bash\nx\n```"))).toMatchObject({ language: "bash" });
    expect(firstCode(parseMessageMarkdown("~~~{.py}\nx\n~~~"))).toMatchObject({ language: "py" });
    expect(firstCode(parseMessageMarkdown("```\nx\n```"))).toMatchObject({ text: "x", language: undefined });
  });

  test("a fence still open at the end of the text is code to the end and says it is not closed", () => {
    expect(firstCode(parseMessageMarkdown("```mermaid\nflowchart TD\n  A-->B"))).toMatchObject({
      language: "mermaid",
      text: "flowchart TD\n  A-->B",
      closed: false,
    });
    expect(firstCode(parseMessageMarkdown("```mermaid"))).toMatchObject({ closed: false });
    expect(firstCode(parseMessageMarkdown("```mermaid\nA-->B\n```"))).toMatchObject({ closed: true });
    expect(firstCode(parseMessageMarkdown("````md\n```\ninner\n```\n````"))).toMatchObject({
      text: "```\ninner\n```",
      closed: true,
    });
  });

  test("an indented code block has no language and counts as closed", () => {
    expect(firstCode(parseMessageMarkdown("para\n\n    indented()"))).toMatchObject({
      text: "indented()",
      language: undefined,
      closed: true,
    });
  });

  test("a JSON object pasted without a fence becomes a json block; prose with braces does not", () => {
    expect(parseMessageMarkdown('{"a": 1,\n "b": [1, 2]}')).toEqual([
      { type: "code", text: '{"a": 1,\n "b": [1, 2]}', language: "json", closed: true },
    ]);
    expect(parseMessageMarkdown("[1, 2, 3]")[0]).toMatchObject({ type: "code", language: "json" });
    expect(parseMessageMarkdown("{not json}")[0].type).toBe("paragraph");
    expect(parseMessageMarkdown("[link]")[0].type).toBe("paragraph");
    expect(parseMessageMarkdown("- [1, 2]")[0].type).toBe("list");
  });
});
