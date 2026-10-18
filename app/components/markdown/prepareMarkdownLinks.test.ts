// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { prepareMarkdownLinks } from "./prepareMarkdownLinks";

describe("Markdown bare URL autolinks", () => {
  test("wraps a URL that sits beside Chinese text", () => {
    expect(prepareMarkdownLinks("项目（https://onlora.ai/p/x）已交付，隧道 https://a.trycloudflare.com（:8774）。")).toBe(
      "项目（<https://onlora.ai/p/x>）已交付，隧道 <https://a.trycloudflare.com>（:8774）。",
    );
  });

  test("leaves code, existing links and plain text as written", () => {
    const source = [
      "`https://e.com/code` and [a](https://e.com/a) and <https://e.com/b>",
      "",
      "```",
      "https://e.com/block",
      "```",
      "",
      "~/.mewla/a and git@github.com:org/repo",
    ].join("\n");
    expect(prepareMarkdownLinks(source)).toBe(source);
  });

  test("reaches lists, quotes, tables and bold", () => {
    expect(
      prepareMarkdownLinks("- 见https://e.com/l，好\n\n> 引用 https://e.com/q，好\n\n| a |\n| - |\n| 表https://e.com/t，|\n\n**https://e.com/b**"),
    ).toBe(
      "- 见<https://e.com/l>，好\n\n> 引用 <https://e.com/q>，好\n\n| a |\n| - |\n| 表<https://e.com/t>，|\n\n**<https://e.com/b>**",
    );
  });

  test("keeps the text of a www link while naming its destination", () => {
    expect(prepareMarkdownLinks("see www.example.com.")).toBe("see [www.example.com](<http://www.example.com>).");
  });

  test("shows links that would do nothing as their text", () => {
    const openable = (href: string) => /^https?:/.test(href);
    expect(
      prepareMarkdownLinks(
        "[js](javascript:alert(1)) [file](docs/a.md) **[b](data:x)** [ok](https://e.com/a) <javascript:x>",
        openable,
      ),
    ).toBe("js file **b** [ok](https://e.com/a) javascript:x");
  });

  test("returns text without URLs untouched", () => {
    expect(prepareMarkdownLinks("# Title\n\nnothing here")).toBe("# Title\n\nnothing here");
  });
});
