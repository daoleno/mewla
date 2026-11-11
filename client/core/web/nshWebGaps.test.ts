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

describe("what NSH 0.4.0's browser runtime lacks", () => {
  // `lifecycle` is not exported in a browser: a call throws and the web app never starts
  // (nsh test's runtime has it, so only a real browser showed it). ui/lifecycle.ts stands in.
  test("lifecycle comes from ui/lifecycle.ts, never from nsh", () => {
    const offenders = sources(UI)
      .filter((path) => !path.endsWith(join("ui", "lifecycle.ts")))
      .filter((path) => /import\s*\{[^}]*\blifecycle\b[^}]*\}\s*from\s*"nsh"/.test(readFileSync(path, "utf8")))
      .map((path) => relative(UI, path));
    expect(offenders).toEqual([]);
  });
});
