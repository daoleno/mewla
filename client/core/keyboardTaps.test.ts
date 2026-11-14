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

/** Each `<Scroll …>` / `<List …>` opening tag (to its `>` outside braces), with its line. */
function scrollTags(source: string): { line: number; tag: string }[] {
  const tags: { line: number; tag: string }[] = [];
  for (const match of source.matchAll(/<(Scroll|List)(?=[\s>])/g)) {
    let depth = 0;
    let end = match.index! + match[0].length;
    for (; end < source.length; end++) {
      const c = source[end];
      if (c === "{") depth++;
      else if (c === "}") depth--;
      else if (c === ">" && depth === 0) break;
    }
    tags.push({ line: source.slice(0, match.index).split("\n").length, tag: source.slice(match.index, end) });
  }
  return tags;
}

describe("a focused field never swallows a tap", () => {
  // NSH's scroll views default to keyboardShouldPersistTaps="never": while a field has focus (it
  // keeps it after Back hides the keyboard on Android) a tap only dismisses the keyboard, so Pair,
  // Save, a chip or a date button did nothing (FINAL-VERIFY 6387f88f: Pair a computer, the
  // Calendar editor, Snooze on a Work page). NSH 0.5.1 lets the tap through once the keyboard is down
  // (longbridge/nsh#1755), but with it up "never" still only dismisses it. Every scroll view says what a tap does.
  test("every Scroll and List sets keyboardShouldPersistTaps", () => {
    const missing = sources(UI).flatMap((path) =>
      scrollTags(readFileSync(path, "utf8"))
        .filter(({ tag }) => !/\bkeyboardShouldPersistTaps=/.test(tag))
        .map(({ line }) => `${relative(UI, path)}:${line}`),
    );
    expect(missing).toEqual([]);
  });
});
