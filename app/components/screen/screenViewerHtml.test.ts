import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { buildScreenViewerHtml } from "./screenViewerHtml";

describe("screen viewer document", () => {
  const html = buildScreenViewerHtml();
  test("cannot reach the network or navigate", () => {
    expect(html).toContain("connect-src 'none'");
    expect(html).toContain("default-src 'none'");
    expect(html).toContain("form-action 'none'");
  });
  test("inlines exactly one module script", () => {
    expect(html.match(/<script/g)?.length).toBe(1);
    expect(html.match(/<\/script>/g)?.length).toBe(1);
  });
  test("generated bundles carry no top-level await Metro or WebViews would reject", () => {
    for (const file of ["screenViewerSource.js", "screenViewerRuntime.js"]) {
      const source = readFileSync(join(import.meta.dir, file), "utf8");
      expect(source).not.toContain("await _checkWebCodecsH264DecodeSupport");
    }
  });
});
