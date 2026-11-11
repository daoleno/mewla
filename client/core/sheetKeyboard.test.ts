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

describe("a sheet's field is above the keyboard", () => {
  // NSH's bottom sheet does not move for the software keyboard: the Commit and Ask sheets' fields and
  // buttons sat behind it (FINAL-VERIFY 6387f88f, gitfiles G1 / F2). Sheets come from shell/AppSheet.tsx,
  // which lifts every one of them; the crash report sheet has no field.
  test("only shell/AppSheet.tsx uses NSH's Sheet, and each of its Sheets is lifted over the keyboard", () => {
    const direct = sources(UI)
      .filter((path) => /import\s*\{[^}]*\bSheet\b[^}]*\}\s*from\s*"nsh\/base"/.test(readFileSync(path, "utf8")))
      .map((path) => relative(UI, path))
      .filter((path) => path !== join("shell", "AppSheet.tsx") && path !== join("crash", "CrashReports.tsx"));
    expect(direct).toEqual([]);
    const sheets = readFileSync(join(UI, "shell", "AppSheet.tsx"), "utf8").match(/<Sheet\b[^>]*>/g) ?? [];
    expect(sheets.length).toBeGreaterThan(0);
    expect(sheets.filter((tag) => !tag.includes("popupStyle={aboveKeyboard(props)}"))).toEqual([]);
  });
});
