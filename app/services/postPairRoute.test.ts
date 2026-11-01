import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { POST_PAIR_ROUTE } from "./postPairRoute";

const read = (path: string) => readFileSync(join(import.meta.dir, "..", path), "utf8");

describe("landing after pairing", () => {
  test("is Brain, the index tab of (primary), not Sessions", () => {
    expect(POST_PAIR_ROUTE).toBe("/");
    expect(read("app/(primary)/_layout.tsx")).toContain('<TopTabs.Screen name="index" options={{ title: "Brain" }} />');
  });

  test("a connect link and the Settings pairing flow both land there", () => {
    const link = read("app/_layout.tsx");
    const imported = link.slice(link.indexOf("onImported: async (importedServer) => {"));
    expect(imported.slice(0, 600)).toContain("router");
    expect(imported.slice(0, 600)).toContain("POST_PAIR_ROUTE");
    expect(imported.slice(0, 600)).not.toContain('"/list"');
    expect(read("app/settings.tsx")).toContain("router.replace(POST_PAIR_ROUTE)");
  });
});
