import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { parse } from "acorn";
import { PIERRE_DIFFS_SOURCE } from "./pierreDiffsSource";
import { PIERRE_TREES_SOURCE } from "./pierreTreesSource";

// The viewers run in whatever WebView the phone has. The floor is iOS 17.5 (the
// deployment target, app/plugins/withIOSBuild.js) and Android WebView 123, the
// release that brought light-dark(). The libraries' CSS ships as written; these
// checks fail only when a library bump needs something newer than that line.
const read = (file: string) => readFileSync(join(import.meta.dir, file), "utf8");
const bundles: Record<string, string> = {
  "diffs runtime": read("pierreDiffsRuntime.js"),
  "diffs source": PIERRE_DIFFS_SOURCE,
  "trees runtime": read("pierreTreesRuntime.js"),
  "trees source": PIERRE_TREES_SOURCE,
};

describe("viewer bundles stay within the platform floor", () => {
  for (const [name, code] of Object.entries(bundles)) {
    test(`${name}: no CSS newer than Safari 17.5 / WebView 123`, () => {
      for (const feature of ["anchor-name", "position-anchor", "field-sizing", "interpolate-size", "calc-size(", "text-box-trim", "@view-transition", "sibling-index(", "corner-shape"]) {
        expect(code).not.toContain(feature);
      }
    });

    test(`${name}: no JS API newer than Safari 17.5 / WebView 123`, () => {
      for (const api of ["Iterator.from", "RegExp.escape", "Promise.try", "Error.isError", "Float16Array", "Math.sumPrecise", "fromBase64", "toBase64", "Math.f16round"]) {
        expect(code).not.toContain(api);
      }
    });
  }

  test("the generated code parses as ES2024", () => {
    for (const code of [bundles["diffs runtime"], bundles["trees runtime"]]) {
      // A newer syntax throws here.
      expect(() => parse(code, { ecmaVersion: 2024, sourceType: "module" })).not.toThrow();
    }
  });
});
