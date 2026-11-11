import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const UI = join(import.meta.dir, "../../ui");

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (name === "node_modules" || name.startsWith(".") || name === "testing") return [];
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : [];
  });
}

describe("a viewer document's base URL", () => {
  // In a browser the page's CSP (`base-uri 'self'`) refused https://viewer.mewla.invalid/ in the
  // srcdoc iframe: three console violations on Screen and a file preview (FAILURES R4 #10).
  test("comes only from ui/viewerBase.ts, which sets none in a browser", () => {
    const base = readFileSync(join(UI, "viewerBase.ts"), "utf8");
    expect(base).toMatch(/platform\.os === "web" \? undefined : "https:\/\/viewer\.mewla\.invalid\/"/);
    const offenders = sources(UI)
      .filter((path) => /baseUrl=/.test(readFileSync(path, "utf8")))
      .filter((path) => /baseUrl=(?!\{viewerBaseUrl\})/.test(readFileSync(path, "utf8")))
      .map((path) => relative(UI, path));
    expect(offenders).toEqual([]);
  });
});
