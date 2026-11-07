import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { parse } from "acorn";
import { PIERRE_DIFFS_SOURCE } from "./pierreDiffsSource";
import { PIERRE_TREES_SOURCE } from "./pierreTreesSource";

// The viewers run in whatever WebView the phone has. The floor is iOS 16.4 (the
// deployment target in app/ios/Podfile) and Android WebView 117 (the diff grid
// uses subgrid). generate-pierre-viewer.mjs lowers the libraries' newer CSS;
// these checks keep anything newer from coming back with a library bump.
const read = (file: string) => readFileSync(join(import.meta.dir, file), "utf8");
const bundles: Record<string, string> = {
  "diffs runtime": read("pierreDiffsRuntime.js"),
  "diffs source": PIERRE_DIFFS_SOURCE,
  "trees runtime": read("pierreTreesRuntime.js"),
  "trees source": PIERRE_TREES_SOURCE,
};

// Shiki's `defaultColor: "light-dark()"` option is dead code here (Pierre passes
// defaultColor: false); it names the function in strings but never emits it.
const withoutShikiOption = (code: string) => code.replace(/light-dark\(\)|light-dark\(\$\{/g, "");

describe("viewer bundles stay within the platform floor", () => {
  for (const [name, code] of Object.entries(bundles)) {
    test(`${name}: no CSS newer than Safari 16.4 / WebView 117`, () => {
      const css = withoutShikiOption(code);
      expect(css).not.toContain("light-dark(");
      expect(css).not.toContain("rgb(from");
      expect(css).not.toContain("hsl(from");
      // CSS nesting (Safari 17.2) starts a rule with `&`; JS has no statement that does.
      expect(/[{};,]\s*&(?!&)[[:>.+~ #]/.test(css)).toBe(false);
      for (const feature of ["@starting-style", "@scope", "anchor-name", "field-sizing", "interpolate-size", ":popover-open"]) {
        expect(css).not.toContain(feature);
      }
    });

    test(`${name}: no JS API newer than Safari 16.4 / WebView 117`, () => {
      for (const api of ["Promise.withResolvers", "Object.groupBy", "Map.groupBy", "URL.canParse", "Iterator.from", "RegExp.escape", "AbortSignal.any", "checkVisibility(", ".intersection(", ".isSubsetOf("]) {
        expect(code).not.toContain(api);
      }
    });
  }

  test("the generated code parses as ES2022", () => {
    for (const code of [bundles["diffs runtime"], bundles["trees runtime"]]) {
      // A newer regular-expression flag (v) or syntax throws here.
      expect(() => parse(code, { ecmaVersion: 2022, sourceType: "module" })).not.toThrow();
    }
  });

  test("lowered light-dark() has its theme switch", () => {
    for (const code of [bundles["diffs runtime"], bundles["trees runtime"]]) {
      expect(code).toContain("--lightningcss-light");
      expect(code).toContain("--lightningcss-dark");
      expect(code).toContain("data-mewla-scheme");
    }
  });
});
