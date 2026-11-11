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

/** Each opening tag (to its `>` outside braces) whose style can set `display: "flex"`, with its line. */
function displayFlexTags(source: string): { line: number; tag: string }[] {
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
    if (/\bdisplay:[^,}]*"flex"/.test(tag)) tags.push({ line: source.slice(0, match.index).split("\n").length, tag });
  }
  return tags;
}

describe("a view shown and hidden by display", () => {
  // NSH follows CSS: a view whose style says `display: "flex"` lays its children out in a row
  // unless a direction is set (a view that sets no display is a column). A section toggled with
  // display: hidden() ? "none" : "flex" put its caption beside its rows, off a phone's screen
  // (FINAL-VERIFY 6387f88f, extensions H1/H5–H7; the Skill inspector's files, K9).
  test("says its direction whenever its style can set display: flex", () => {
    const missing = sources(UI).flatMap((path) =>
      displayFlexTags(readFileSync(path, "utf8"))
        .filter(({ tag }) => !/\bflex-(col|row)(-reverse)?\b/.test(tag) && !/\bflexDirection:/.test(tag))
        .map(({ line }) => `${relative(UI, path)}:${line}`),
    );
    expect(missing).toEqual([]);
  });
});
