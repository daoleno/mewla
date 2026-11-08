import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { parseDiffViewerMessage } from "./diffViewerProtocol";
import { parseTreeViewerMessage } from "./treeViewerProtocol";
import { PIERRE_FONT_FAMILY, buildPierreViewerHtml } from "./pierreViewerHtml";

describe("diff reader messages", () => {
  test("well-formed reader messages parse", () => {
    expect(parseDiffViewerMessage('{"type":"ready"}')).toEqual({ type: "ready" });
    expect(parseDiffViewerMessage('{"type":"loaded","key":"k","hunks":3,"ms":12}')).toMatchObject({ type: "loaded", hunks: 3 });
    expect(parseDiffViewerMessage('{"type":"needFiles","id":"0-unstaged"}')).toEqual({ type: "needFiles", id: "0-unstaged" });
    expect(parseDiffViewerMessage('{"type":"find","query":"q","count":2,"index":-1}')).toMatchObject({ count: 2, index: -1 });
    expect(parseDiffViewerMessage('{"type":"error","message":"boom"}')).toEqual({ type: "error", message: "boom" });
    expect(parseDiffViewerMessage('{"type":"selection","section":"0-unstaged","side":"additions","start":4,"end":6}')).toMatchObject({ section: "0-unstaged", start: 4, end: 6 });
    expect(parseDiffViewerMessage('{"type":"selection","section":null,"side":"additions","start":0,"end":0}')).toMatchObject({ section: null });
  });

  test("a malformed selection is dropped", () => {
    expect(parseDiffViewerMessage('{"type":"selection","section":3,"side":"additions","start":1,"end":1}')).toBeNull();
    expect(parseDiffViewerMessage('{"type":"selection","section":"s","side":"left","start":1,"end":1}')).toBeNull();
    expect(parseDiffViewerMessage('{"type":"selection","section":"s","side":"deletions","start":1.5,"end":2}')).toBeNull();
  });

  test("anything unexpected is dropped", () => {
    expect(parseDiffViewerMessage("not json")).toBeNull();
    expect(parseDiffViewerMessage('{"type":"delete-everything"}')).toBeNull();
    expect(parseDiffViewerMessage('{"type":"loaded","key":"k","hunks":"3","ms":1}')).toBeNull();
    expect(parseDiffViewerMessage('{"type":"needFiles"}')).toBeNull();
    expect(parseDiffViewerMessage('{"type":"find","query":"q","count":1.5,"index":0}')).toBeNull();
    expect(parseDiffViewerMessage(`{"type":"error","message":"${"x".repeat(100_001)}"}`)).toBeNull();
    expect(parseDiffViewerMessage(undefined as unknown as string)).toBeNull();
  });
});

describe("file tree messages", () => {
  test("well-formed tree messages parse", () => {
    expect(parseTreeViewerMessage('{"type":"open","path":"a/b.ts"}')).toEqual({ type: "open", path: "a/b.ts" });
    expect(parseTreeViewerMessage('{"type":"menu","path":"a","kind":"directory"}')).toMatchObject({ kind: "directory" });
    expect(parseTreeViewerMessage('{"type":"ready"}')).toEqual({ type: "ready" });
  });

  test("anything unexpected is dropped", () => {
    expect(parseTreeViewerMessage('{"type":"open"}')).toBeNull();
    expect(parseTreeViewerMessage('{"type":"open","path":""}')).toBeNull();
    expect(parseTreeViewerMessage('{"type":"menu","path":"a","kind":"symlink"}')).toBeNull();
    expect(parseTreeViewerMessage('{"type":"count","query":"x","count":4}')).toBeNull();
    expect(parseTreeViewerMessage('{"type":"navigate","url":"https://example.com"}')).toBeNull();
  });
});

describe("viewer document", () => {
  const html = buildPierreViewerHtml({ source: "window.x=1;/*</script>*/", fontUri: "file:///fonts/Mono's.ttf", background: "#101010" });

  test("cannot reach the network or navigate", () => {
    expect(html).toContain("connect-src 'none'");
    expect(html).toContain("default-src 'none'");
    expect(html).toContain("form-action 'none'");
  });

  test("inlines exactly one script, even when the source mentions a closing tag", () => {
    expect(html.match(/<script/g)?.length).toBe(1);
    expect(html.match(/<\/script>/g)?.length).toBe(1);
  });

  test("the terminal font is the one family it declares, with the URI escaped", () => {
    expect(html).toContain(`font-family:'${PIERRE_FONT_FAMILY}'`);
    expect(html).toContain("Mono\\'s.ttf");
    expect(buildPierreViewerHtml({ source: "", fontUri: null, background: "#fff" })).not.toContain("@font-face");
  });

  test("the background cannot break out of its declaration", () => {
    expect(buildPierreViewerHtml({ source: "", fontUri: null, background: "red;}</style><script>x()" }).match(/<script/g)?.length).toBe(1);
  });
});

describe("generated runtimes", () => {
  const read = (file: string) => readFileSync(join(import.meta.dir, file), "utf8");
  for (const file of ["pierreDiffsSource.js", "pierreDiffsRuntime.js", "pierreTreesSource.js", "pierreTreesRuntime.js"]) {
    test(`${file} fetches no code and loads no WASM at run time`, () => {
      const source = read(file);
      expect(/(?<![\w$.\\])import\(\s*(["'`]|[A-Za-z_$])/.test(source)).toBe(false);
      expect(/new URL\([^)]*import\.meta/.test(source)).toBe(false);
      expect(source.includes(".wasm\"")).toBe(false);
    });
  }

  test("the diff reader ships its fixed grammar set and stays small", () => {
    const source = read("pierreDiffsRuntime.js");
    // Booleans, so a failure never prints the multi-megabyte bundle.
    expect(source.includes("source.ts")).toBe(true);
    expect(source.includes("source.go")).toBe(true);
    // Shipping Shiki's themes and every grammar is 11 MB; the trimmed set is about 3 MB.
    expect(source.length).toBeLessThan(3_600_000);
    expect(source.includes("source.elisp")).toBe(false);
  });
});
