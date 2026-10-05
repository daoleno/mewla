import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const appRoot = join(import.meta.dir, "../..");

// Chevrons that page content rather than navigate back, and the Git diff
// chrome whose leading slot swaps Close/Back in its own filled button family.
const NOT_BACK = new Set([
  "app/calendar.tsx", // Previous month
  "components/terminal/ZenImage.tsx", // Previous image
  "components/terminal/GitDiffSheetTopChrome.tsx",
  "components/navigation/HeaderBackButton.tsx",
]);

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx?$/.test(name) && !/\.test\./.test(name) ? [path] : [];
  });
}

describe("back affordance", () => {
  test("every back control is the shared HeaderBackButton", () => {
    const offenders = [...sources(join(appRoot, "app")), ...sources(join(appRoot, "components"))]
      .map((path) => relative(appRoot, path))
      .filter((path) => !NOT_BACK.has(path))
      .filter((path) =>
        /"chevron-back"|"arrow-back|label="Back"|>Back</.test(
          readFileSync(join(appRoot, path), "utf8"),
        ),
      );
    expect(offenders).toEqual([]);
  });

  test("the root Stack owns one default header Back", () => {
    const layout = readFileSync(join(appRoot, "app/_layout.tsx"), "utf8");
    expect(layout.match(/headerLeft:/g)).toHaveLength(1);
    expect(layout).toContain("<StackBackButton");
    expect(layout).toContain("...webHeaderInsets");
    expect(layout).toContain('headerTitleAlign: "center"');
  });
});
