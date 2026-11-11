import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const UI = join(import.meta.dir, "../../ui");

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (name === "node_modules" || name.startsWith(".")) return [];
    if (statSync(path).isDirectory()) return sources(path);
    return /\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : [];
  });
}

describe("what NSH's browser runtime lacks", () => {
  // NSH 0.5.0 exports `lifecycle` in a browser (0.4.0 did not; ui/lifecycle.ts stood in until then).
  // BackHandler is not in the browser runtime (still undefined in 0.5.0's): Settings → Pair a computer threw
  // "he.on is not a function" on the web (FINAL-VERIFY 6387f88f, page:Settings → Pair a computer).
  test("BackHandler.on is called only behind a web check", () => {
    const offenders = sources(UI)
      .filter((path) => /BackHandler\.on\(/.test(readFileSync(path, "utf8")))
      .filter((path) => !/platform\.os === "web"\) return;\s*cleanup\(BackHandler\.on\(/.test(readFileSync(path, "utf8")))
      .map((path) => relative(UI, path));
    expect(offenders).toEqual([]);
  });
});
