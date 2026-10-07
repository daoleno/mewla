import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { ZEN_BRAND_COLORS, ZEN_DARK_APP_COLORS, ZEN_LIGHT_APP_COLORS } from "../../theme/primitives";
import { MEWLA_BRAND_ASSETS } from "./mewlaBrandArt";

const app = join(import.meta.dir, "../..");
const branding = join(app, "assets/branding");
const { expo: base } = JSON.parse(readFileSync(join(app, "app.base.json"), "utf8"));
// eslint-disable-next-line @typescript-eslint/no-require-imports
const createConfig = require("../../app.config.js") as () => { plugins: unknown[] };

/** Width, height and PNG colour type (2 RGB, 0 grey: no alpha; 6 RGBA). */
function pngHeader(file: string) {
  const bytes = readFileSync(file);
  return { width: bytes.readUInt32BE(16), height: bytes.readUInt32BE(20), colorType: bytes[25] };
}

describe("Mewla brand assets", () => {
  test("committed SVG sources match the art (run bun scripts/render-mewla-brand.ts)", () => {
    for (const asset of MEWLA_BRAND_ASSETS) {
      const source = readFileSync(join(branding, "source", `${asset.name}.svg`), "utf8");
      expect(source).toBe(asset.art());
    }
  });

  test("each PNG is rendered at its size, and App Store icons carry no alpha", () => {
    for (const asset of MEWLA_BRAND_ASSETS) {
      const header = pngHeader(join(branding, `${asset.name}.png`));
      expect([header.width, header.height]).toEqual([asset.size, asset.size]);
      if ("opaque" in asset) expect([0, 2]).toContain(header.colorType);
    }
  });

  test("app config uses the Mewla assets in the theme's colours", () => {
    const paths = [
      base.icon,
      ...Object.values(base.ios.icon),
      base.android.adaptiveIcon.foregroundImage,
      base.android.adaptiveIcon.monochromeImage,
      base.web.favicon,
    ];
    for (const path of paths) {
      expect(path).toMatch(/^\.\/assets\/branding\/mewla-[a-z-]+\.png$/);
      expect(existsSync(join(app, path))).toBe(true);
    }
    expect(base.splash).toBeUndefined();
    expect(base.android.adaptiveIcon.backgroundColor).toBe(ZEN_BRAND_COLORS.vermilion);

    const plugin = (name: string) =>
      (createConfig().plugins.find((entry) => Array.isArray(entry) && entry[0] === name) as [string, Record<string, any>])[1];
    const splash = plugin("expo-splash-screen");
    expect(splash.image).toBe("./assets/branding/mewla-splash.png");
    expect(splash.dark.image).toBe(splash.image);
    // Android 12+ masks the splash icon to a 192 dp circle: the square seal must fit inside it.
    expect(splash.imageWidth * Math.SQRT2).toBeLessThanOrEqual(192);
    expect(splash.backgroundColor).toBe(ZEN_LIGHT_APP_COLORS.bgPrimary);
    expect(splash.dark.backgroundColor).toBe(ZEN_DARK_APP_COLORS.bgPrimary);

    const notifications = plugin("expo-notifications");
    expect(notifications.icon).toBe("./assets/branding/mewla-notification.png");
    expect(notifications.color).toBe(ZEN_BRAND_COLORS.vermilion);
  });
});
