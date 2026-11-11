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

/** Each `renderImage={…}` callback (to its closing brace), with its line. */
function renderImages(source: string): { line: number; body: string }[] {
  const found: { line: number; body: string }[] = [];
  for (const match of source.matchAll(/renderImage=\{/g)) {
    let depth = 1;
    let end = match.index! + match[0].length;
    for (; end < source.length && depth > 0; end++) {
      if (source[end] === "{") depth++;
      else if (source[end] === "}") depth--;
    }
    found.push({ line: source.slice(0, match.index).split("\n").length, body: source.slice(match.index, end) });
  }
  return found;
}

describe("a picture inside a text view", () => {
  // A text view sizes what renderImage gives it to its content, so a picture that only says
  // 100% / w-full came out 0 wide (FINAL-VERIFY 6387f88f K9, SKILL.md; the same in a chat reply).
  test("every renderImage picture is given the text's measured width", () => {
    const unsized = sources(UI).flatMap((path) =>
      renderImages(readFileSync(path, "utf8"))
        .filter(({ body }) => !/\bwidth=\{/.test(body))
        .map(({ line }) => `${relative(UI, path)}:${line}`),
    );
    expect(unsized).toEqual([]);
  });
});
