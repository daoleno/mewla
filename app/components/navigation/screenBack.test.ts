import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { resolveScreenBack, screenBackParent } from "./screenBack";

describe("screen back", () => {
  test("pops when history exists and lands on the parent otherwise", () => {
    expect(resolveScreenBack({ canGoBack: true })).toBe("pop");
    expect(resolveScreenBack({ canGoBack: false })).toBe("parent");
  });

  test("deep-linked screens land on their logical parent", () => {
    expect(screenBackParent("model-profiles")).toBe("/settings");
    for (const route of [
      "calendar",
      "skills",
      "stats",
      "browser",
      "resources",
      "settings",
      "work/[id]",
      // Nested Plugins stack
      "index",
      "custom",
      "[service]/index",
      "[service]/permissions",
    ]) {
      expect(screenBackParent(route)).toBe("/");
    }
  });

  test("headerless root routes get no default header Back", () => {
    const layout = readFileSync(
      join(import.meta.dir, "../../app/_layout.tsx"),
      "utf8",
    );
    const screens = [
      ...layout.matchAll(
        /<Stack\.Screen\s+name="([^"]+)"\s+options=\{\{([^}]*)\}\}/g,
      ),
    ];
    expect(screens.length).toBeGreaterThan(0);
    for (const [, name, options] of screens) {
      const headerless = /headerShown:\s*false/.test(options);
      expect({ name, noDefaultBack: screenBackParent(name) === null }).toEqual({
        name,
        noDefaultBack: headerless,
      });
    }
  });
});
