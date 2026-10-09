import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { BRAND_COLORS, DARK_APP_COLORS, LIGHT_APP_COLORS } from "../../theme/primitives";

const app = join(import.meta.dir, "../..");
const branding = join(app, "assets/branding");
const { expo: base } = JSON.parse(readFileSync(join(app, "app.base.json"), "utf8"));
// eslint-disable-next-line @typescript-eslint/no-require-imports
const createConfig = require("../../app.config.js") as () => { plugins: unknown[] };

/** The brand PNGs (generated art, see docs/third-party-assets.md) and their sizes. */
const ASSETS = [
  { name: "mewla-icon", size: 1024, opaque: true },
  { name: "mewla-icon-dark", size: 1024, opaque: true },
  { name: "mewla-icon-tinted", size: 1024 },
  { name: "mewla-adaptive-foreground", size: 432 },
  { name: "mewla-adaptive-monochrome", size: 432 },
  { name: "mewla-notification", size: 96 },
  { name: "mewla-favicon", size: 48 },
  { name: "mewla-splash", size: 512 },
];

/** Width, height and PNG colour type (2 RGB, 0 grey: no alpha; 6 RGBA). */
function pngHeader(file: string) {
  const bytes = readFileSync(file);
  return { width: bytes.readUInt32BE(16), height: bytes.readUInt32BE(20), colorType: bytes[25] };
}

describe("Mewla brand assets", () => {
  test("each PNG has its size, and App Store icons carry no alpha", () => {
    for (const asset of ASSETS) {
      const header = pngHeader(join(branding, `${asset.name}.png`));
      expect([header.width, header.height]).toEqual([asset.size, asset.size]);
      if (asset.opaque) expect([0, 2]).toContain(header.colorType);
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
    // The adaptive foreground sits on the icon's paper.
    expect(base.android.adaptiveIcon.backgroundColor).toBe("#F6F1E8");

    const plugin = (name: string) =>
      (createConfig().plugins.find((entry) => Array.isArray(entry) && entry[0] === name) as [string, Record<string, any>])[1];
    const splash = plugin("expo-splash-screen");
    expect(splash.image).toBe("./assets/branding/mewla-splash.png");
    expect(splash.dark.image).toBe(splash.image);
    // Android 12+ masks the splash icon to a 192 dp circle: the round seal must fit inside it.
    expect(splash.imageWidth * Math.SQRT2).toBeLessThanOrEqual(192);
    expect(splash.backgroundColor).toBe(LIGHT_APP_COLORS.bgPrimary);
    expect(splash.dark.backgroundColor).toBe(DARK_APP_COLORS.bgPrimary);

    const notifications = plugin("expo-notifications");
    expect(notifications.icon).toBe("./assets/branding/mewla-notification.png");
    expect(notifications.color).toBe(BRAND_COLORS.vermilion);
  });
});
