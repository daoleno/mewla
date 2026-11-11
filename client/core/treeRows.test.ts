import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const UI = join(import.meta.dir, "../ui");

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (name === "node_modules" || name.startsWith(".")) return [];
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx$/.test(name) && !/\.test\.tsx$/.test(name) ? [path] : [];
  });
}

/** Each JSX opening tag (to its `>` outside braces) that names `onLongPress`, with its line. */
function longPressTags(source: string): { line: number; tag: string }[] {
  const tags: { line: number; tag: string }[] = [];
  for (const match of source.matchAll(/<[A-Z][A-Za-z.]*(?=[\s>])/g)) {
    let depth = 0;
    let end = match.index! + match[0].length;
    for (; end < source.length; end++) {
      const c = source[end];
      if (c === "{") depth++;
      else if (c === "}") depth--;
      else if (c === ">" && depth === 0) break;
    }
    const tag = source.slice(match.index, end);
    // The tree's own tag holds its renderItem: the rows are checked on their own.
    if (match[0] !== "<Tree" && /\bonLongPress=/.test(tag)) tags.push({ line: source.slice(0, match.index).split("\n").length, tag });
  }
  return tags;
}

describe("a tree's rows take a tap", () => {
  // On a phone a row that hears long presses (its menu) takes the touch from the tree around it, so a
  // tap opened no folder and no file (FINAL-VERIFY 6387f88f, gitfiles I2 / I10). nsh test presses the
  // nearest view that hears presses, so only this rule sees it.
  test("a row drawn by a Tree that hears long presses hears presses too", () => {
    const missing = sources(UI)
      .filter((path) => /<Tree\b/.test(readFileSync(path, "utf8")))
      .flatMap((path) =>
        longPressTags(readFileSync(path, "utf8"))
          .filter(({ tag }) => !/\bonPress=/.test(tag))
          .map(({ line }) => `${relative(UI, path)}:${line}`),
      );
    expect(missing).toEqual([]);
  });
});
