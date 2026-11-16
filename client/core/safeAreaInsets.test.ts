import { expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

/**
 * The screen's insets are read from `safeArea()`, never CSS `env(safe-area-inset-*)`.
 *
 * env() reads the insets of the page a view is declared in, and a page is already clear of the status bar (the
 * shell pads it), so it reads 0 there: a full-screen dialog or sheet declared in a page drew its top row under
 * Android's status bar (Mac re-run 11-14, 3c: Rename's Save; Brain 11-16: the full-screen video's Close in a Friends
 * post). `safeArea()` is the window's, wherever it is read.
 */

const UI = join(import.meta.dir, "../ui");

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (name === "node_modules" || name.startsWith(".")) return [];
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : [];
  });
}

test("no view reads the screen's insets through env(safe-area-inset-*)", () => {
  const found = sources(UI).flatMap((path) =>
    readFileSync(path, "utf8").split("\n").flatMap((line, at) =>
      // A comment may name it (AppSheet.tsx says why it is not used).
      /^\s*(\*|\/\/)/.test(line) || !/env\(\s*safe-area-inset/.test(line) ? [] : [`${relative(UI, path)}:${at + 1}`]));
  expect(found).toEqual([]);
});
