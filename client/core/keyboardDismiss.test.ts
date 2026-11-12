import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// The soft keyboard outlived its field (FAILURES R4#4): a sheet closed after Commit / Ask / a new
// branch, a page left or covered, Back in a terminal that raised it. Headless `nsh test` has no
// keyboard, so these guard where the app lets it go; the device replay is in FIXES-ROUND4.md.
const read = (path: string) => readFileSync(join(import.meta.dir, "../ui", path), "utf8");

describe("the keyboard goes down with its field", () => {
  test("a sheet that closes dismisses it (every AppSheet, GuardedSheet and GuardedDialog shares guardedOpen)", () => {
    const sheet = read("shell/AppSheet.tsx");
    const guarded = sheet.slice(sheet.indexOf("function guardedOpen"), sheet.indexOf("function aboveKeyboard"));
    expect(guarded).toMatch(/if \(!open\) \{[\s\S]*?if \(closed\) Keyboard\.dismiss\(\);/);
    expect(guarded).toMatch(/dismissed\.set\(true\);\s*Keyboard\.dismiss\(\);/);
  });

  test("leaving a page (a pop) and changing tabs dismiss it", () => {
    const shell = read("shell/Shell.tsx");
    const paged = shell.slice(shell.indexOf("function Paged"), shell.indexOf("function Section"));
    // Before the phone's early return: phones take that branch.
    expect(paged.indexOf("Keyboard.dismiss()")).toBeGreaterThan(-1);
    expect(paged.indexOf("Keyboard.dismiss()")).toBeLessThan(paged.indexOf("if (mobile) return"));
    expect(paged).toMatch(/if \(now < depth\) Keyboard\.dismiss\(\);/);
    const shellFn = shell.slice(shell.indexOf("export function Shell()"));
    expect(shellFn.slice(0, shellFn.indexOf("if (!mobile)"))).toMatch(/if \(tab !== shownTab\) Keyboard\.dismiss\(\);/);
  });

  test("a composer whose page is covered lets go of its field", () => {
    expect(read("chat/Composer.tsx")).toMatch(/if \(!props\.active \|\| props\.active\(\)\) return;[\s\S]{0,80}if \(focused\(\)\) input\?\.blur\(\);/);
  });

  test("Back in a terminal hides the terminal's own keyboard before leaving the page", () => {
    const pane = read("terminal/TerminalPane.tsx");
    expect(pane).toMatch(/onBackPress\(\(\) => \{\s*if \(!props\.active\(\) \|\| !keyboard\(\) \|\| keyboardFrame\(\)\.height <= 0\) return false;\s*command\("blur", \{\}\);/);
    expect(pane).toMatch(/if \(platform\.os === "web"\) return;\s*cleanup\(BackHandler\.on\("press", listener\)\);/);
  });
  test("opening a file from Browse files (its search field may hold the keyboard) dismisses it (R5#4)", () => {
    const files = read("more/git/GitFiles.tsx");
    const open = files.slice(files.indexOf("const openFile ="), files.indexOf("const tapped ="));
    expect(open).toMatch(/Keyboard\.dismiss\(\);\s*props\.nav\.push\(\{ name: "git-file"/);
  });
});
