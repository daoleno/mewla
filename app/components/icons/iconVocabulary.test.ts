import { describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { MEWLA_GLYPHS } from "./mewlaGlyphs";
import { PHOSPHOR_GLYPHS } from "./phosphorGlyphs";

const appRoot = join(import.meta.dir, "../..");
const sources = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "--", "."], {
  cwd: appRoot,
  encoding: "utf8",
})
  .split("\n")
  .filter((path) => /\.(?:ts|tsx|js|jsx)$/.test(path) && !/\.(?:test|spec)\./.test(path));

describe("icon vocabulary", () => {
  test("every glyph has drawable geometry and Mewla glyphs don't shadow vendored ones", () => {
    for (const paths of Object.values(PHOSPHOR_GLYPHS)) {
      expect(paths.length).toBeGreaterThan(0);
      for (const d of paths) expect(d).toMatch(/^M[\d.,\s\-A-Za-z]+$/);
    }
    for (const glyph of Object.values(MEWLA_GLYPHS)) expect(glyph.strokes.length).toBeGreaterThan(0);
    for (const name of Object.keys(MEWLA_GLYPHS)) expect(name in PHOSPHOR_GLYPHS).toBe(false);
  });

  test("icon fonts remain only for real product logos", () => {
    const users = sources.filter((path) => readFileSync(join(appRoot, path), "utf8").includes("@expo/vector-icons"));
    expect(users.sort()).toEqual([
      "components/plugins/PluginConnectionViews.tsx",
      "components/terminal/AgentKindIcon.tsx",
    ]);
    const plugins = readFileSync(join(appRoot, "components/plugins/PluginConnectionViews.tsx"), "utf8");
    expect(plugins.match(/<Ionicons\b/g)).toHaveLength(1);
    expect(plugins).toContain("<Ionicons name={SERVICE_LOGOS[id]}");
  });
});
