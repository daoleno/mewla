import { describe, expect, test } from "bun:test";
import { inlinePartsText } from "../markdown/markdownInline";
import {
  holdBackHalfTypedMarker,
  prepareInterfaceMarkdown,
} from "./InterfaceNativeMarkdownBodyPrepare";
import {
  splitIntoChunks,
  messageImages,
  messageTableCellText,
  parseMessageMarkdown,
  type MessageBlock,
  type MessageListBlock,
} from "./InterfaceMessageMarkdownModel";
import { MESSAGE_KITCHEN_SINK, longMessage } from "./messageBodyFixtures";

const text = (block: MessageBlock | undefined) =>
  block && (block.type === "paragraph" || block.type === "heading") ? inlinePartsText(block.inline) : "";
const list = (blocks: MessageBlock[], at = 0) => {
  const block = blocks[at];
  if (block?.type !== "list") throw new Error(`block ${at} is ${block?.type}`);
  return block as MessageListBlock;
};
const streamed = (value: string) => parseMessageMarkdown(prepareInterfaceMarkdown(value, true));

describe("GFM tables", () => {
  const table = (value: string) => {
    const block = parseMessageMarkdown(value)[0];
    if (block?.type !== "table") throw new Error(`block is ${block?.type}`);
    return block;
  };

  test("headers, alignments and rows, with inline Markdown in the cells", () => {
    const block = table("| Name | Count | Note |\n| :-- | --: | :-: |\n| a | 1 | `ok` |\n| b | 22 | **bold** |");
    expect(block.headers.map(messageTableCellText)).toEqual(["Name", "Count", "Note"]);
    expect(block.alignments).toEqual(["left", "right", "center"]);
    expect(block.rows.map((row) => row.map(messageTableCellText))).toEqual([
      ["a", "1", "ok"],
      ["b", "22", "bold"],
    ]);
    expect(block.rows[0][2]).toEqual([{ kind: "code", text: "ok" }]);
    expect(block.rows[1][2]).toEqual([{ kind: "bold", text: "bold" }]);
  });

  test("an escaped pipe stays in its cell, in code too (GFM splits a bare pipe even there)", () => {
    const block = table("| a | b |\n| --- | --- |\n| x \\| y | `p\\|q` |");
    expect(block.rows[0].map(messageTableCellText)).toEqual(["x | y", "p|q"]);
  });

  test("a short row is padded to the header width and a <br> is a line break", () => {
    const block = table("| a | b |\n| --- | --- |\n| only |\n| one<br>two | z |");
    expect(block.rows[0].map(messageTableCellText)).toEqual(["only", ""]);
    expect(messageTableCellText(block.rows[1][0])).toBe("one\ntwo");
  });

  test("a table without outer pipes", () => {
    expect(table("a | b\n--- | ---\n1 | 2").rows.map((row) => row.map(messageTableCellText))).toEqual([["1", "2"]]);
  });

  test("a table ends at the first blank line and the text after it is a paragraph", () => {
    const blocks = parseMessageMarkdown("| a |\n| --- |\n| 1 |\n\nafter");
    expect(blocks.map((b) => b.type)).toEqual(["table", "paragraph"]);
  });

  test("a header row without its delimiter row is only a paragraph", () => {
    expect(parseMessageMarkdown("| a | b |\n| 1 | 2 |")[0].type).toBe("paragraph");
  });
});

describe("GFM task lists, strikethrough and nested lists", () => {
  test("task items carry their state and keep their text", () => {
    const items = list(parseMessageMarkdown("- [ ] open\n- [x] done\n- [X] also done\n- plain")).items;
    expect(items.map((item) => item.taskState)).toEqual(["unchecked", "checked", "checked", undefined]);
    expect(items.map((item) => text(item.blocks[0]))).toEqual(["open", "done", "also done", "plain"]);
  });

  test("a task item in a loose list and a task item with a nested list", () => {
    const items = list(parseMessageMarkdown("- [x] a\n\n  more\n\n- [ ] b\n  - sub")).items;
    expect(items[0].taskState).toBe("checked");
    expect(items[0].blocks.map(text)).toEqual(["a", "more"]);
    expect(items[1].blocks.map((b) => b.type)).toEqual(["paragraph", "list"]);
  });

  test("strikethrough is a styled part and survives nesting", () => {
    const block = parseMessageMarkdown("a ~~gone~~ b ~~**both**~~")[0];
    expect(block.type === "paragraph" && block.inline).toEqual([
      { text: "a " },
      { kind: "strike", text: "gone" },
      { text: " b " },
      { kind: "strike", text: "both", children: [{ kind: "bold", text: "both" }] },
    ]);
  });

  test("lists nest to any depth, each with its own numbering", () => {
    const top = list(parseMessageMarkdown("1. one\n   - a\n     1. deep\n     2. deeper\n   - b\n2. two"));
    expect(top.ordered).toBe(true);
    expect(top.items).toHaveLength(2);
    const bullets = top.items[0].blocks[1] as MessageListBlock;
    expect(bullets.ordered).toBe(false);
    expect(bullets.items).toHaveLength(2);
    const deep = bullets.items[0].blocks[1] as MessageListBlock;
    expect([deep.ordered, deep.start, deep.items.length]).toEqual([true, 1, 2]);
  });

  test("an ordered list keeps the number it starts at", () => {
    expect(list(parseMessageMarkdown("3. c\n4. d")).start).toBe(3);
    expect(list(parseMessageMarkdown("- x")).ordered).toBe(false);
  });

  test("a code block and a quote inside a list item stay in that item", () => {
    const items = list(parseMessageMarkdown("- a\n\n  ```sh\n  ls\n  ```\n\n  > q\n- b")).items;
    expect(items[0].blocks.map((b) => b.type)).toEqual(["paragraph", "code", "quote"]);
    expect(items).toHaveLength(2);
  });

  test("a line under a list item's text continues that item (lazy continuation)", () => {
    const items = list(parseMessageMarkdown("- first\ncontinued\n- second")).items;
    expect(items.map((item) => text(item.blocks[0]))).toEqual(["first continued", "second"]);
  });
});

describe("entities, escapes, links and quotes in prose", () => {
  const first = (value: string) => {
    const block = parseMessageMarkdown(value)[0];
    return block?.type === "paragraph" ? block.inline : [];
  };

  test("HTML entities read as the characters they name", () => {
    expect(inlinePartsText(first("a &amp; b &lt;c&gt; &copy; &#35; &#x1F600; &mdash; &nbsp;|"))).toBe(
      "a & b <c> © # \u{1F600} —  |",
    );
  });

  test("an unknown entity and a lone ampersand stay as typed; code is never decoded", () => {
    expect(inlinePartsText(first("&bogus; and AT&T and &amp"))).toBe("&bogus; and AT&T and &amp");
    expect(first("`&amp;`")).toEqual([{ kind: "code", text: "&amp;" }]);
    expect(first("```\n&amp;\n```")).toEqual([]);
    const code = parseMessageMarkdown("```\n&amp;\n```")[0];
    expect(code.type === "code" && code.text).toBe("&amp;");
  });

  test("a backslash escape is the character, and an escaped entity is not decoded", () => {
    expect(inlinePartsText(first("a \\* b \\<tag\\> \\&amp; c"))).toBe("a * b <tag> &amp; c");
  });

  test("autolinks: bare URL, www, <url>, and an address; sentence punctuation stays out", () => {
    expect(first("see https://example.com/a_b, and <https://example.com/x> or www.example.com or me@example.com.")).toEqual([
      { text: "see " },
      { kind: "link", text: "https://example.com/a_b", url: "https://example.com/a_b" },
      { text: ", and " },
      { kind: "link", text: "https://example.com/x", url: "https://example.com/x" },
      { text: " or " },
      { kind: "link", text: "www.example.com", url: "http://www.example.com" },
      { text: " or " },
      { kind: "link", text: "me@example.com", url: "mailto:me@example.com" },
      { text: "." },
    ]);
  });

  test("a bare URL ends at CJK text", () => {
    const parts = first("见https://example.com/guide然后看");
    expect(parts[1]).toMatchObject({ kind: "link", url: "https://example.com/guide" });
    expect(parts[2]).toEqual({ text: "然后看" });
  });

  test("curly quotes in prose become straight ones, but not inside code", () => {
    expect(inlinePartsText(first("it’s “quoted”"))).toBe("it's \"quoted\"");
    expect(first("`‘x’`")).toEqual([{ kind: "code", text: "‘x’" }]);
  });

  test("HTML comments are dropped and CRLF is a newline", () => {
    expect(parseMessageMarkdown("a <!-- hidden --> b\r\n\r\nnext").map(text)).toEqual(["a  b", "next"]);
  });

  test("an HTML block shows as the text it is", () => {
    expect(parseMessageMarkdown("<div>hi</div>\n\nafter").map(text)).toEqual(["<div>hi</div>", "after"]);
  });

  test("blank input is no blocks", () => {
    expect(parseMessageMarkdown("")).toEqual([]);
    expect(parseMessageMarkdown(" \n\n ")).toEqual([]);
  });
});

describe("images", () => {
  test("an image on its own line is a block, with the prose around it kept", () => {
    expect(parseMessageMarkdown('before ![cat](a.png "Caption") after')).toEqual([
      { type: "paragraph", inline: [{ text: "before" }] },
      { type: "image", src: "a.png", alt: "cat", title: "Caption" },
      { type: "paragraph", inline: [{ text: "after" }] },
    ]);
  });

  test("an image inside a list item or a quote stays inside it, and the gallery lists every image in order", () => {
    const blocks = parseMessageMarkdown("![one](1.png)\n\n- item\n\n  ![two](2.png)\n\n> ![three](3.png)\n\n![four][ref]\n\n[ref]: 4.png");
    expect(messageImages(blocks).map((image) => image.src)).toEqual(["1.png", "2.png", "3.png", "4.png"]);
    const item = list(blocks, 1).items[0];
    expect(item.blocks.map((b) => b.type)).toEqual(["paragraph", "image"]);
  });

  test("an image in code or escaped is text", () => {
    expect(messageImages(parseMessageMarkdown("`![x](a.png)` and \\![y](b.png)"))).toEqual([]);
  });
});

describe("blocks are reused between parses", () => {
  test("a block whose text did not change is the same object, so only the growing block re-renders", () => {
    const first = parseMessageMarkdown("# Title\n\nstable paragraph\n\ngrowing");
    const second = parseMessageMarkdown("# Title\n\nstable paragraph\n\ngrowing more");
    expect(second[0]).toBe(first[0]);
    expect(second[1]).toBe(first[1]);
    expect(second[2]).not.toBe(first[2]);
  });

  test("a message with a link reference definition is parsed afresh, since the definition changes its paragraphs", () => {
    const before = parseMessageMarkdown("[a][r] x");
    const after = parseMessageMarkdown("[a][r] x\n\n[r]: https://example.com");
    expect(before[0].type === "paragraph" && before[0].inline[0]).toEqual({ text: "[a][r] x" });
    expect(after[0].type === "paragraph" && after[0].inline[0]).toMatchObject({ kind: "link", url: "https://example.com" });
  });

  test("a person's message and an agent's, with the same text, do not share blocks", () => {
    expect(parseMessageMarkdown("a\nb", { keepLineBreaks: true })[0]).not.toBe(parseMessageMarkdown("a\nb")[0]);
  });
});

describe("streaming prefixes", () => {
  const headings = (blocks: MessageBlock[]): number => blocks.filter((b) => b.type === "heading").length;

  test("a paragraph followed by the start of a list never flashes as a heading", () => {
    const body = "Intro paragraph\n- first item\n- second item\n\nOutro\n1. one\n2. two";
    for (let n = 1; n <= body.length; n += 1) {
      expect(headings(streamed(body.slice(0, n)))).toBe(0);
    }
    // the unprepared parse is the failure this guards against
    expect(headings(parseMessageMarkdown("Intro paragraph\n-"))).toBe(1);
  });

  test("a bare marker line is held back, an empty bullet or quote never shows", () => {
    const empty = (blocks: MessageBlock[]): boolean =>
      blocks.some(
        (b) =>
          (b.type === "list" && b.items.some((item) => item.blocks.length === 0)) ||
          (b.type === "quote" && b.blocks.length === 0),
      );
    const body = "Some text\n\n- one\n- two\n\n1. a\n2. b\n\n> quoted\n> more";
    for (let n = 1; n <= body.length; n += 1) {
      expect(empty(streamed(body.slice(0, n)))).toBe(false);
    }
  });

  test("the held-back line is only the last one, and only a bare marker", () => {
    expect(holdBackHalfTypedMarker("text\n-")).toBe("text\n");
    expect(holdBackHalfTypedMarker("text\n  1.")).toBe("text\n");
    expect(holdBackHalfTypedMarker("text\n>")).toBe("text\n");
    expect(holdBackHalfTypedMarker("text\n=")).toBe("text\n");
    expect(holdBackHalfTypedMarker("text\n- item")).toBe("text\n- item");
    expect(holdBackHalfTypedMarker("- a\n-\n- b")).toBe("- a\n-\n- b");
    expect(holdBackHalfTypedMarker("-")).toBe("");
  });

  test("a lone - inside an open code fence is code, not a marker", () => {
    expect(holdBackHalfTypedMarker("```diff\n-")).toBe("```diff\n-");
    expect(holdBackHalfTypedMarker("```diff\n-\n```\n-")).toBe("```diff\n-\n```\n");
  });

  test("an open fence is code to the end at every prefix; its closing fence arrives last", () => {
    const body = "intro\n\n```mermaid\nflowchart LR\n  A-->B\n```\n\nafter";
    for (let n = 1; n <= body.length; n += 1) {
      const code = streamed(body.slice(0, n)).find((b) => b.type === "code");
      if (!code || code.type !== "code") continue;
      expect(code.closed).toBe(n >= body.indexOf("```\n\nafter") + 3);
    }
  });

  test("open inline syntax is closed while streaming, so ** or ` never shows", () => {
    expect(streamed("a **bold")[0]).toMatchObject({ inline: [{ text: "a " }, { kind: "bold", text: "bold" }] });
    expect(streamed("run `code")[0]).toMatchObject({ inline: [{ text: "run " }, { kind: "code", text: "code" }] });
  });

  test("a streamed block that is finished equals the same block parsed whole", () => {
    const body = MESSAGE_KITCHEN_SINK;
    const whole = parseMessageMarkdown(body);
    const final = streamed(body);
    expect(final).toEqual(whole);
  });

  test("while a table streams in, the blocks before it never change", () => {
    const body = "Intro\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n| 3 | 4 |";
    const intro = parseMessageMarkdown("Intro")[0];
    for (let n = "Intro\n\n".length + 1; n <= body.length; n += 1) {
      expect(streamed(body.slice(0, n))[0]).toBe(intro);
    }
  });
});

describe("the reference message", () => {
  test("renders every kind of block once", () => {
    const blocks = parseMessageMarkdown(MESSAGE_KITCHEN_SINK);
    const kinds = new Set(blocks.map((b) => b.type));
    for (const kind of ["heading", "paragraph", "quote", "list", "table", "code", "rule"]) {
      expect(kinds.has(kind as MessageBlock["type"])).toBe(true);
    }
    expect(blocks.filter((b) => b.type === "code").map((b) => b.type === "code" && b.language)).toEqual(["ts", "mermaid"]);
  });
});

describe("a long message is read in pieces that never cut a block", () => {
  const section = (n: number) =>
    `## Part ${n}\n\n- one ${n}\n\n- two ${n}\n\n- three ${n}\n\n\`\`\`ts\nconst a = ${n};\n\nconst b = 2;\n\`\`\`\n\n> quote ${n}\n>\n> more\n\n1. first\n2. second\n\nclosing paragraph ${n} ${"filler ".repeat(30)}\n`;
  const doc = Array.from({ length: 40 }, (_, n) => section(n)).join("\n");

  test("it is split, the pieces join back to the text, and no piece opens inside a list or a fence", () => {
    const chunks = splitIntoChunks(doc);
    expect(chunks.length).toBeGreaterThan(5);
    expect(chunks.join("\n")).toBe(doc);
    for (const chunk of chunks.slice(1)) {
      expect(/^[ \t]|^(?:[-*+]|\d+[.)])[ \t]/.test(chunk)).toBe(false);
    }
  });

  test("the blocks are those of each part: loose lists whole, blank lines kept inside code", () => {
    const blocks = parseMessageMarkdown(doc);
    expect(blocks.map((b) => b.type).join(",")).toBe(
      Array.from({ length: 40 }, () => "heading,list,code,quote,list,paragraph").join(","),
    );
    const lists = blocks.filter((b) => b.type === "list") as MessageListBlock[];
    expect(lists.filter((l) => !l.ordered).every((l) => l.items.length === 3)).toBe(true);
    const code = blocks.find((b) => b.type === "code");
    expect(code?.type === "code" && code.text).toBe("const a = 0;\n\nconst b = 2;");
  });

  test("a list that continues after a blank line is never split from its next item", () => {
    const body = `${"filler ".repeat(300)}\n\n- a\n\n- b\n\n${"x ".repeat(500)}\n\n- c\n\n- d`;
    const lists = parseMessageMarkdown(body).filter((b) => b.type === "list") as MessageListBlock[];
    expect(lists.map((l) => l.items.length)).toEqual([2, 2]);
    const continued = `- a\n\n  ${"x ".repeat(800)}\n\n- b`;
    expect((parseMessageMarkdown(continued)[0] as MessageListBlock).items.length).toBe(2);
  });

  test("a link definition or a raw <pre> block keeps the message whole", () => {
    const long = `${doc}\n[ref]: https://example.com\n`;
    expect(splitIntoChunks(long)).toEqual([long]);
    const pre = `${doc}\n<pre>\na\n\nb\n</pre>\n`;
    expect(splitIntoChunks(pre)).toEqual([pre]);
  });

  test("HTML in prose or inside a fence does not keep a long message whole", () => {
    const prose = `${doc}\nUse a \`<script>\` tag or <pre> inline.\n`;
    expect(splitIntoChunks(prose).length).toBeGreaterThan(5);
    const fenced = `${doc}\n\`\`\`html\n<!DOCTYPE html>\n<script>x()</script>\n[a]: not a definition\n\`\`\`\n`;
    expect(splitIntoChunks(fenced).length).toBeGreaterThan(5);
    const indented = `${doc}\n    <pre>\n`;
    expect(splitIntoChunks(indented).length).toBeGreaterThan(5);
  });

  test("a marker still being typed on the last line is not treated as margin text", () => {
    const body = `${"filler ".repeat(300)}\n\n- a`;
    expect(splitIntoChunks(body)).toEqual([body]);
  });

  test("a message several thousand lines long is read in well under a second", () => {
    const text = longMessage(250) + Array.from({ length: 400 }, (_, n) => section(n)).join("\n");
    expect(text.split("\n").length).toBeGreaterThan(8000);
    const started = performance.now();
    const blocks = parseMessageMarkdown(text);
    expect(blocks.length).toBeGreaterThan(2000);
    expect(performance.now() - started).toBeLessThan(1500);
  });
});
