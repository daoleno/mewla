// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  dirnameOf,
  openMarkdownLink,
  resolveAgainst,
  resolveMarkdownLink,
  tokenizeMarkdownLinks,
} from "./markdownLinks";

describe("Markdown fallback links", () => {
  test("retains surrounding Markdown while exposing link destinations", () => {
    expect(tokenizeMarkdownLinks("See **[docs](https://example.com/docs)** now."))
      .toEqual([
        { text: "See **" },
        { text: "docs", url: "https://example.com/docs" },
        { text: "** now." },
      ]);
  });

  test("finds bare, angle and parenthesised URLs", () => {
    expect(tokenizeMarkdownLinks("a https://e.com/x. b <https://e.com/y> [w](https://e.com/Foo_(bar))"))
      .toEqual([
        { text: "a " },
        { text: "https://e.com/x", url: "https://e.com/x" },
        { text: ". b " },
        { text: "https://e.com/y", url: "https://e.com/y" },
        { text: " " },
        { text: "w", url: "https://e.com/Foo_(bar)" },
      ]);
  });

  test("exposes unsafe destinations for policy rejection", () => {
    expect(tokenizeMarkdownLinks("See [unsafe](javascript:alert) now."))
      .toEqual([
        { text: "See " },
        { text: "unsafe", url: "javascript:alert" },
        { text: " now." },
      ]);
    expect(resolveMarkdownLink("javascript:alert")).toEqual({ kind: "blocked" });
  });
});

describe("Markdown link policy", () => {
  test("opens web, mail and phone destinations outside the app", () => {
    for (const url of [
      "https://example.com/docs?q=a&b=c#frag",
      "http://localhost:8080/path",
      "mailto:user@example.com",
      "tel:+10000000",
    ]) {
      expect(resolveMarkdownLink(` ${url} `)).toEqual({ kind: "external", url });
    }
  });

  test("blocks schemes that can run code or launch apps", () => {
    for (const url of [
      "javascript:alert(1)",
      "JAVASCRIPT:alert(1)",
      "data:text/html,unsafe",
      "vbscript:x",
      "blob:https://example.com/id",
      "intent://scan#Intent;end",
      "#fragment",
      "",
    ]) {
      expect(resolveMarkdownLink(url)).toEqual({ kind: "blocked" });
    }
  });

  test("treats a destination without a scheme as a file", () => {
    expect(resolveMarkdownLink("app.apk")).toEqual({ kind: "file", reference: "app.apk" });
    expect(resolveMarkdownLink("./docs/report.pdf")).toEqual({
      kind: "file",
      reference: "./docs/report.pdf",
    });
    expect(resolveMarkdownLink("/home/me/work/out.zip")).toEqual({
      kind: "file",
      reference: "/home/me/work/out.zip",
    });
    expect(resolveMarkdownLink("~/notes/a.md")).toEqual({ kind: "file", reference: "~/notes/a.md" });
    expect(resolveMarkdownLink("my%20file.pdf#page=2")).toEqual({
      kind: "file",
      reference: "my file.pdf",
    });
    expect(resolveMarkdownLink("src/app.ts:42:7")).toEqual({
      kind: "file",
      reference: "src/app.ts",
    });
    expect(resolveMarkdownLink("file:///tmp/out%20a.txt")).toEqual({
      kind: "file",
      reference: "/tmp/out a.txt",
    });
    expect(resolveMarkdownLink("file://other-host/tmp/a.txt")).toEqual({ kind: "blocked" });
  });

  test("anchors relative files at the document directory", () => {
    expect(resolveMarkdownLink("../img/a.png", "/w/docs/guide")).toEqual({
      kind: "file",
      reference: "/w/docs/img/a.png",
    });
    expect(resolveMarkdownLink("a.md", "worklog/x")).toEqual({
      kind: "file",
      reference: "worklog/x/a.md",
    });
    expect(resolveMarkdownLink("/abs/a.md", "/w/docs")).toEqual({
      kind: "file",
      reference: "/abs/a.md",
    });
    expect(resolveAgainst("", "a.md")).toBe("a.md");
    expect(dirnameOf("/w/docs/a.md")).toBe("/w/docs");
    expect(dirnameOf("a.md")).toBe("");
  });

  test("maps a daemon file URL to the file it serves", () => {
    expect(
      resolveMarkdownLink(
        "https://box.example.com/session-file?worker_id=%251&path=%2Ftmp%2Fout.zip&generation=g",
      ),
    ).toEqual({ kind: "file", reference: "/tmp/out.zip" });
    expect(resolveMarkdownLink("https://box.example.com/other?path=/x")).toEqual({
      kind: "external",
      url: "https://box.example.com/other?path=/x",
    });
  });
});

describe("Markdown link activation", () => {
  test("opens trimmed external links and routes files to the file opener", async () => {
    const opened: string[] = [];
    const files: string[] = [];
    const actions = {
      openUrl: async (url: string) => void opened.push(url),
      openFile: (reference: string) => {
        files.push(reference);
        return true;
      },
    };

    expect(await openMarkdownLink(" https://example.com/docs ", actions)).toBe(true);
    expect(await openMarkdownLink("release.zip", actions)).toBe(true);
    expect(opened).toEqual(["https://example.com/docs"]);
    expect(files).toEqual(["release.zip"]);
  });

  test("never hands a file or a blocked link to the URL opener", async () => {
    const opened: string[] = [];
    const actions = { openUrl: async (url: string) => void opened.push(url) };

    expect(await openMarkdownLink("docs/a.md", actions)).toBe(false);
    for (const url of ["javascript:alert(1)", "data:text/html,x", "#top", "not a URL:"]) {
      expect(await openMarkdownLink(url, actions)).toBe(false);
    }
    expect(opened).toEqual([]);
  });

  test("contains openURL rejection", async () => {
    const result = await openMarkdownLink("https://example.com", {
      openUrl: async () => {
        throw new Error("no URL handler");
      },
    });

    expect(result).toBe(false);
  });
});
