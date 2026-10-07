#!/usr/bin/env bun
// Render the Mewla brand assets from app/components/mewla/mewlaBrandArt.ts:
// each SVG source goes to app/assets/branding/source/ and its PNG to
// app/assets/branding/ (two folders keep Android resource names distinct).
// Needs rsvg-convert (librsvg) and ImageMagick. Run from anywhere:
//   bun scripts/render-mewla-brand.ts
import { spawnSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { MEWLA_BRAND_ASSETS } from "../app/components/mewla/mewlaBrandArt";

const out = join(import.meta.dir, "../app/assets/branding");

function run(command: string, args: string[]) {
  const result = spawnSync(command, args, { stdio: "inherit" });
  if (result.status !== 0) throw new Error(`${command} failed: ${args.join(" ")}`);
}

for (const asset of MEWLA_BRAND_ASSETS) {
  const source = join(out, "source", `${asset.name}.svg`);
  writeFileSync(source, asset.art());
  const size = String(asset.size);
  const png = join(out, `${asset.name}.png`);
  run("rsvg-convert", ["-w", size, "-h", size, "-o", png, source]);
  if ("opaque" in asset) run("magick", [png, "-alpha", "off", png]);
  console.log(`${asset.name}.png ${size}×${size}`);
}
