import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const UI = join(import.meta.dir, "../ui");

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (name === "node_modules" || name.startsWith(".")) return [];
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : [];
  });
}

/**
 * A bottom sheet's panel is drawn over the whole window, so its content keeps clear of the window's sides, which
 * AppSheet adds to the sheet's own `sidePadding` (`sheetSides`). A side padding in its class would be replaced by that,
 * and `env(safe-area-inset-left/right)` reads the insets of the page the sheet is declared in: 0 beside the iPhone Duo's
 * bar, where Rename's Save and Brain's menu sat (Mac re-run 11-14, 3c).
 */
describe("a bottom sheet keeps clear of the window's sides", () => {
  test("no sheet sets its side padding in its class", () => {
    const sheets = /popupClass="([^"]*)"|const SHEET = "([^"]*)"/g;
    const found = sources(UI).flatMap((path) => {
      const source = readFileSync(path, "utf8");
      return [...source.matchAll(sheets)]
        .filter((match) => {
          // A menu's popover (nsh/base Menu, on the popover surface) is placed by its anchor, inside the page: not a sheet.
          const classes = match[1] ?? match[2] ?? "";
          return !/\bbg-popover\b/.test(classes) && /(^|\s)(px|pl|pr|p)-\S+/.test(classes);
        })
        .map((match) => `${relative(UI, path)}: ${match[0]}`);
    });
    expect(found).toEqual([]);
  });

  test("nothing reads the page's side insets with env()", () => {
    const found = sources(UI).filter((path) => /env\(safe-area-inset-(left|right)\)/.test(readFileSync(path, "utf8"))).map((path) => relative(UI, path));
    expect(found).toEqual([]);
  });
});
