import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { statusBarForeground } from "./statusBar";

const UI = join(import.meta.dir, "../../ui");

describe("the status bar follows the app's appearance on every page", () => {
  test("dark icons on the light page, light icons on the dark one", () => {
    expect(statusBarForeground("light")).toBe("dark");
    expect(statusBarForeground("dark")).toBe("light");
  });

  test("the app's NavPage and the pages that use NSH's NavPage directly state it", () => {
    const appPage = readFileSync(join(UI, "shell", "NavPage.tsx"), "utf8");
    expect(appPage).toContain("statusBar={pageStatusBar(own.statusBar)}");
    for (const file of [join("phone", "PhonePage.tsx")]) {
      expect(readFileSync(join(UI, file), "utf8")).toContain("statusBar={pageStatusBar()}");
    }
  });
});
