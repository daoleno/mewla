import { describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { MEWLA_GLYPHS } from "./mewlaGlyphs";

const appRoot = join(import.meta.dir, "../..");
const sources = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "--", "."], {
  cwd: appRoot,
  encoding: "utf8",
})
  .split("\n")
  .filter((path) => /\.(?:ts|tsx|js|jsx)$/.test(path) && !/\.(?:test|spec)\./.test(path));

type Point = [number, number];

/** Every point a path passes through or pulls toward, arcs sampled along their sweep. */
function pathPoints(d: string): Point[] {
  const tokens = d.match(/[A-Za-z]|-?\d*\.?\d+/g) ?? [];
  const points: Point[] = [];
  let [x, y, startX, startY] = [0, 0, 0, 0];
  let command = "";
  let i = 0;
  const num = () => Number(tokens[i++]);
  while (i < tokens.length) {
    if (/[A-Za-z]/.test(tokens[i])) command = tokens[i++];
    const rel = command === command.toLowerCase();
    const ox = rel ? x : 0;
    const oy = rel ? y : 0;
    switch (command.toUpperCase()) {
      case "M":
      case "L":
        [x, y] = [ox + num(), oy + num()];
        if (command.toUpperCase() === "M") [startX, startY] = [x, y];
        break;
      case "H":
        x = ox + num();
        break;
      case "V":
        y = oy + num();
        break;
      case "Q":
        points.push([ox + num(), oy + num()]);
        [x, y] = [ox + num(), oy + num()];
        break;
      case "C":
        points.push([ox + num(), oy + num()], [ox + num(), oy + num()]);
        [x, y] = [ox + num(), oy + num()];
        break;
      case "S":
        points.push([ox + num(), oy + num()]);
        [x, y] = [ox + num(), oy + num()];
        break;
      case "A": {
        const [r, , , large, sweep] = [num(), num(), num(), num(), num()];
        const [toX, toY] = [ox + num(), oy + num()];
        points.push(...arcSamples([x, y], [toX, toY], r, large === 1, sweep === 1));
        [x, y] = [toX, toY];
        break;
      }
      case "Z":
        [x, y] = [startX, startY];
        break;
      default:
        throw new Error(`unexpected path command ${command} in ${d}`);
    }
    points.push([x, y]);
  }
  return points;
}

function arcSamples(from: Point, to: Point, radius: number, large: boolean, sweep: boolean): Point[] {
  const [mx, my] = [(from[0] - to[0]) / 2, (from[1] - to[1]) / 2];
  const r = Math.max(radius, Math.hypot(mx, my));
  const factor = Math.sqrt(Math.max(0, r * r - mx * mx - my * my) / (mx * mx + my * my)) * (large === sweep ? -1 : 1);
  const center: Point = [(from[0] + to[0]) / 2 + factor * my, (from[1] + to[1]) / 2 - factor * mx];
  const a0 = Math.atan2(from[1] - center[1], from[0] - center[0]);
  let delta = Math.atan2(to[1] - center[1], to[0] - center[0]) - a0;
  if (sweep && delta < 0) delta += Math.PI * 2;
  if (!sweep && delta > 0) delta -= Math.PI * 2;
  return Array.from({ length: 33 }, (_, k): Point => {
    const a = a0 + (delta * k) / 32;
    return [center[0] + r * Math.cos(a), center[1] + r * Math.sin(a)];
  });
}

describe("icon vocabulary", () => {
  test("every glyph is drawable path data with plain two-decimal numbers", () => {
    for (const [name, glyph] of Object.entries(MEWLA_GLYPHS)) {
      const fills: readonly string[] = "fills" in glyph ? glyph.fills : [];
      expect(glyph.strokes.length + fills.length, name).toBeGreaterThan(0);
      for (const d of [...glyph.strokes, ...fills]) {
        expect(d, name).toMatch(/^M[MLHVQCSAZmlhvqcsaz\d.,\s-]+$/);
        expect(d, name).not.toMatch(/\d\.\d{3}|-0(?![.\d])/);
      }
    }
  });

  test("every glyph stays inside the 24 grid, stroke included", () => {
    for (const [name, glyph] of Object.entries(MEWLA_GLYPHS)) {
      const fills: readonly string[] = "fills" in glyph ? glyph.fills : [];
      const inked = [
        ...glyph.strokes.flatMap((d) => pathPoints(d).map((p) => ({ p, pad: 0.75 }))),
        ...fills.flatMap((d) => pathPoints(d).map((p) => ({ p, pad: 0 }))),
      ];
      for (const { p, pad } of inked) {
        expect(Math.min(...p) - pad, `${name} ${p}`).toBeGreaterThanOrEqual(0.74);
        expect(Math.max(...p) + pad, `${name} ${p}`).toBeLessThanOrEqual(23.26);
      }
    }
  });

  test("the Mewla touches stay: Brain has cat ears and the menu is two lines", () => {
    expect(MEWLA_GLYPHS.menu.strokes).toHaveLength(2);
    const brain = pathPoints(MEWLA_GLYPHS.brain.strokes[0]);
    const earTips = brain.filter(([, y]) => y < 4);
    expect(earTips.some(([x]) => x < 9)).toBe(true);
    expect(earTips.some(([x]) => x > 15)).toBe(true);
    expect(brain.some(([x, y]) => x > 9 && x < 15 && y < 4)).toBe(false);
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
