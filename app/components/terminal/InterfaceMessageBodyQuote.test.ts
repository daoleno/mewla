import { describe, expect, test } from "bun:test";
import { inlinePartsText } from "../markdown/markdownInline";
import { parseMessageMarkdown, type MessageBlock } from "./InterfaceMessageMarkdownModel";
import { prepareInterfaceMarkdown } from "./InterfaceNativeMarkdownBodyPrepare";

const REPORTED = `这条你可以直接发：

> Heng，Mac Studio 上你们那套 Lima VM runner（githubrunner + vm-runner-1..6）我们想复用来跑 Perpetuo 的 CI，不另搭一套。想确认几件事：
> 1. 这些 runner 是注册在 freeride-ai 组织级还是 training 仓库级？标签是什么？
> 2. VM 是 Linux 吗？每个 job 用完会重建吗？
> 3. 方便的话我们可以多开 2–3 个实例专门给 Perpetuo，不占你们训练 CI 的名额。需要的话我们出人改 vm_runner.py 的配置，改动会告诉你。
>
> 另外我们今天已经在这台机器上加了一个独立的 runner（用户 ghrunner，名字 mac-studio），没碰你们那套。如果介意可以随时删掉。`;

type Shape = string | [string, Shape[]];
function shape(blocks: MessageBlock[]): Shape[] {
  return blocks.map((b) => (b.type === "quote" ? ["quote", shape(b.blocks)] : b.type));
}
function quoteOf(blocks: MessageBlock[], at = 0) {
  const b = blocks[at];
  if (b?.type !== "quote") throw new Error(`block ${at} is ${b?.type}`);
  return b.blocks;
}

describe("blockquote", () => {
  test("the reported message is one quote: paragraph, list, paragraph", () => {
    const blocks = parseMessageMarkdown(REPORTED);
    expect(shape(blocks)).toEqual(["paragraph", ["quote", ["paragraph", "list", "paragraph"]]]);
    const list = quoteOf(blocks, 1)[1];
    expect(list.type === "list" && list.ordered).toBe(true);
    expect(list.type === "list" && list.items.length).toBe(3);
    // no literal ">" survives anywhere
    expect(JSON.stringify(blocks)).not.toContain('">');
  });

  test("several empty > lines stay in the same quote", () => {
    expect(shape(parseMessageMarkdown("> a\n>\n>\n>\n> b"))).toEqual([["quote", ["paragraph", "paragraph"]]]);
  });

  test("no space after >", () => {
    expect(shape(parseMessageMarkdown(">a\n>\n>b"))).toEqual([["quote", ["paragraph", "paragraph"]]]);
    expect(shape(parseMessageMarkdown(">- x\n>- y"))).toEqual([["quote", ["list"]]]);
  });

  test("a list inside a quote", () => {
    const inner = quoteOf(parseMessageMarkdown("> intro\n> - a\n> - b\n>\n> 1. one\n> 2. two"));
    expect(inner.map((b) => b.type)).toEqual(["paragraph", "list", "list"]);
  });

  test("a code fence inside a quote keeps its blank lines", () => {
    const inner = quoteOf(parseMessageMarkdown("> before\n> ```ts\n> a\n>\n> b\n> ```\n> after"));
    expect(inner.map((b) => b.type)).toEqual(["paragraph", "code", "paragraph"]);
    const code = inner[1];
    expect(code.type === "code" && code.text).toBe("a\n\nb");
    expect(code.type === "code" && code.language).toBe("ts");
  });

  test("an unclosed fence inside a streaming quote is open code, not a split", () => {
    const inner = quoteOf(parseMessageMarkdown("> x\n> ```\n> y\n>"));
    expect(inner.map((b) => b.type)).toEqual(["paragraph", "code"]);
    expect(inner[1]).toMatchObject({ closed: false });
  });

  test("nested quotes", () => {
    expect(shape(parseMessageMarkdown("> outer\n>\n> > inner\n> > more\n>\n> back"))).toEqual([
      ["quote", ["paragraph", ["quote", ["paragraph"]], "paragraph"]],
    ]);
    expect(shape(parseMessageMarkdown(">> deep"))).toEqual([["quote", [["quote", ["paragraph"]]]]]);
  });

  test("a plain paragraph right after the quote, with or without a blank line", () => {
    expect(shape(parseMessageMarkdown("> q\n\nafter"))).toEqual([["quote", ["paragraph"]], "paragraph"]);
    expect(shape(parseMessageMarkdown("> q\n>\n\nafter"))).toEqual([["quote", ["paragraph"]], "paragraph"]);
  });

  test("a line without > right under the quote's text continues it (lazy continuation)", () => {
    const blocks = parseMessageMarkdown("> a\nlazy\n\nafter");
    expect(shape(blocks)).toEqual([["quote", ["paragraph"]], "paragraph"]);
    const inner = quoteOf(blocks);
    expect(inner[0].type === "paragraph" && inlinePartsText(inner[0].inline)).toBe("a lazy");
  });

  test("a blank line without > ends the quote; two quotes stay two", () => {
    expect(shape(parseMessageMarkdown("> a\n\n> b"))).toEqual([["quote", ["paragraph"]], ["quote", ["paragraph"]]]);
  });

  test("streaming prefixes never show a literal > or split the quote", () => {
    for (let n = 1; n <= REPORTED.length; n += 1) {
      const prefix = REPORTED.slice(0, n);
      const walk = (blocks: MessageBlock[]) => {
        for (const b of blocks) {
          if (b.type === "paragraph") expect(inlinePartsText(b.inline).startsWith(">")).toBe(false);
          if (b.type === "quote") walk(b.blocks);
        }
      };
      const blocks = parseMessageMarkdown(prepareInterfaceMarkdown(prefix, true));
      walk(blocks);
      expect(blocks.filter((b) => b.type === "quote").length).toBeLessThanOrEqual(1);
    }
  });

  test("a person's own quote keeps typed line breaks", () => {
    const inner = quoteOf(parseMessageMarkdown("> a\n> b", { keepLineBreaks: true }));
    expect(inner[0].type === "paragraph" && inlinePartsText(inner[0].inline)).toBe("a\nb");
  });

  test("a > line inside a top-level fence is code", () => {
    expect(shape(parseMessageMarkdown("```\n> not a quote\n```"))).toEqual(["code"]);
  });
});
