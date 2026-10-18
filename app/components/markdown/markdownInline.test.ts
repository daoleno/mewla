// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { tokenizeMarkdownInline } from "./markdownInline";

const link = (text: string, url = text, extra = {}) => ({ kind: "link", text, url, ...extra });

describe("Markdown inline links", () => {
  test("links a bare URL without the punctuation that ends the sentence", () => {
    expect(tokenizeMarkdownInline("see https://example.com/docs/guide, then https://example.com/end.")).toEqual([
      { text: "see " },
      link("https://example.com/docs/guide"),
      { text: ", then " },
      link("https://example.com/end"),
      { text: "." },
    ]);
    expect(tokenizeMarkdownInline("(https://example.com/a)")).toEqual([
      { text: "(" },
      link("https://example.com/a"),
      { text: ")" },
    ]);
  });

  test("keeps a bare URL's query string and fragment", () => {
    expect(tokenizeMarkdownInline("https://example.com/s?q=a&b=c#frag")).toEqual([
      link("https://example.com/s?q=a&b=c#frag"),
    ]);
  });

  test("links www and mail addresses", () => {
    expect(tokenizeMarkdownInline("www.example.com me@example.com")).toEqual([
      link("www.example.com", "http://www.example.com"),
      { text: " " },
      link("me@example.com", "mailto:me@example.com"),
    ]);
  });

  test("ends a URL at full-width punctuation", () => {
    expect(tokenizeMarkdownInline("访问 https://example.com/guide，然后看**粗**。")).toEqual([
      { text: "访问 " },
      link("https://example.com/guide"),
      { text: "，然后看" },
      { kind: "bold", text: "粗" },
      { text: "。" },
    ]);
    expect(tokenizeMarkdownInline("（https://example.com/p）结束")).toEqual([
      { text: "（" },
      link("https://example.com/p"),
      { text: "）结束" },
    ]);
  });

  test("reads [text](url), titles and balanced parentheses", () => {
    expect(
      tokenizeMarkdownInline('[w](https://en.wikipedia.org/wiki/Foo_(bar)) [q](https://e.com/s?q=a&b=c "Title")'),
    ).toEqual([
      link("w", "https://en.wikipedia.org/wiki/Foo_(bar)"),
      { text: " " },
      link("q", "https://e.com/s?q=a&b=c"),
    ]);
  });

  test("reads angle-bracket autolinks and destinations", () => {
    expect(tokenizeMarkdownInline("<https://example.com/angle>")).toEqual([
      link("https://example.com/angle"),
    ]);
    expect(tokenizeMarkdownInline("[spaced](<a b.pdf>)")).toEqual([link("spaced", "a b.pdf")]);
  });

  test("keeps file destinations for the link policy", () => {
    expect(tokenizeMarkdownInline("[a](app.apk) [r](./docs/r.pdf) [w](/home/me/w.md)")).toEqual([
      link("a", "app.apk"),
      { text: " " },
      link("r", "./docs/r.pdf"),
      { text: " " },
      link("w", "/home/me/w.md"),
    ]);
  });

  test("nests links in bold, italic and strike", () => {
    expect(tokenizeMarkdownInline("**[b](https://e.com/b)**")).toEqual([
      { kind: "bold", text: "b", children: [link("b", "https://e.com/b")] },
    ]);
    expect(tokenizeMarkdownInline("**https://e.com/bare**")).toEqual([
      { kind: "bold", text: "https://e.com/bare", children: [link("https://e.com/bare")] },
    ]);
    expect(tokenizeMarkdownInline("~~gone~~ *soft* and [**strong**](https://e.com/s)")).toEqual([
      { kind: "strike", text: "gone" },
      { text: " " },
      { kind: "italic", text: "soft" },
      { text: " and " },
      link("strong", "https://e.com/s", { children: [{ kind: "bold", text: "strong" }] }),
    ]);
  });

  test("keeps a URL in inline code as code", () => {
    expect(tokenizeMarkdownInline("run `curl https://example.com/code` now")).toEqual([
      { text: "run " },
      { kind: "code", text: "curl https://example.com/code" },
      { text: " now" },
    ]);
  });

  test("exposes dangerous destinations so the link policy can block them", () => {
    expect(tokenizeMarkdownInline("[js](javascript:alert(1))")).toEqual([
      link("js", "javascript:alert(1)"),
    ]);
  });
});

describe("Markdown inline text that is not a link", () => {
  test("leaves home paths, snake_case and escapes as written", () => {
    expect(tokenizeMarkdownInline("~/.mewla/a and ~/.mewla/b with snake_case_word and a \\* b")).toEqual([
      { text: "~/.mewla/a and ~/.mewla/b with snake_case_word and a * b" },
    ]);
  });

  test("does not read an SSH remote as an email address", () => {
    expect(tokenizeMarkdownInline("clone git@github.com:org/repo now")).toEqual([
      { text: "clone git@github.com:org/repo now" },
    ]);
  });

  test("keeps an unfinished link as text while a reply streams", () => {
    expect(tokenizeMarkdownInline("[text](http")).toEqual([{ text: "[text](http" }]);
    // The partial URL is a bare link until the closing parenthesis arrives.
    expect(tokenizeMarkdownInline("[text](https://exa")).toEqual([
      { text: "[text](" },
      link("https://exa"),
    ]);
  });

  test("keeps ampersands and raw angle brackets", () => {
    expect(tokenizeMarkdownInline("a & b <c>")).toEqual([{ text: "a & b <c>" }]);
  });
});
