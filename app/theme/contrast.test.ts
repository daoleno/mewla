import { describe, expect, test } from "bun:test";
import { parseHex, relativeLuminance, rgbToHex } from "./colorUtils";
import { ZEN_ACCENTS } from "./primitives";
import { resolveTheme } from "./resolve";
import type { ResolvedZenTheme, ThemeColorScheme } from "./types";

// WCAG 2.x AA: body text 4.5:1; large text and meaningful UI affordances
// (focus ring, strong borders, status glyphs, heatmap cells) 3:1.
const TEXT = 4.5;
const UI = 3;

function contrastRatio(foreground: string, background: string): number {
  const a = relativeLuminance(foreground);
  const b = relativeLuminance(background);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

/** Flattens an `rgba(r,g,b,a)` layer onto an opaque hex backdrop. */
function composite(layer: string, backdrop: string): string {
  const match = layer.match(/^rgba\((\d+),\s*(\d+),\s*(\d+),\s*([\d.]+)\)$/);
  if (!match) return layer;
  const [, r, g, b, alpha] = match;
  const base = parseHex(backdrop);
  const a = Number(alpha);
  const mix = (top: number, bottom: number) => Math.round(top * a + bottom * (1 - a));
  return rgbToHex(mix(Number(r), base.red), mix(Number(g), base.green), mix(Number(b), base.blue));
}

function toOklab(hex: string): [number, number, number] {
  const { red, green, blue } = parseHex(hex);
  const lin = (value: number) => {
    const c = value / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  const [r, g, b] = [lin(red), lin(green), lin(blue)];
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
}

/** Perceptual distance (OKLab ΔE); ~0.02 is a just-noticeable difference. */
function oklabDistance(a: string, b: string): number {
  const [l1, a1, b1] = toOklab(a);
  const [l2, a2, b2] = toOklab(b);
  return Math.hypot(l1 - l2, a1 - a2, b1 - b2);
}

type Pairing = [label: string, foreground: string, background: string, minimum: number];

function pairings(theme: ResolvedZenTheme): Pairing[] {
  const { colors: c, chat, materials } = theme;
  const canvases: [string, string][] = [
    ["bgPrimary", c.bgPrimary],
    ["bgSurface", c.bgSurface],
    ["bgElevated", c.bgElevated],
    ["surfaceSubtle", c.surfaceSubtle],
    ["surfacePressed", c.surfacePressed],
    ["modalSurface", c.modalSurface],
    ["modalSurfaceAlt", c.modalSurfaceAlt],
    ["inputBackground", c.inputBackground],
  ];
  const text: Pairing[] = canvases.flatMap(([name, bg]) => [
    [`textPrimary / ${name}`, c.textPrimary, bg, TEXT],
    [`textSecondary / ${name}`, c.textSecondary, bg, TEXT],
    [`textTertiary / ${name}`, c.textTertiary, bg, TEXT],
    [`accent / ${name}`, c.accent, bg, TEXT],
    [`dangerText / ${name}`, c.dangerText, bg, TEXT],
    [`focusRing / ${name}`, c.focusRing, bg, UI],
    [`borderStrong / ${name}`, c.borderStrong, bg, UI],
  ]);
  const chrome = composite(materials.chrome, c.bgPrimary);
  const regular = composite(materials.regular, c.bgPrimary);
  const thick = composite(materials.thick, c.bgPrimary);
  const selection = composite(c.selectionBackground, c.bgSurface);
  const tint = composite(materials.tint, c.bgSurface);
  return [
    ...text,
    ["textOnAccent / accent", c.textOnAccent, c.accent, TEXT],
    ["accentStrong / accentSoft", c.accentStrong, c.accentSoft, TEXT],
    ["textPrimary / surfaceActive", c.textPrimary, c.surfaceActive, TEXT],
    ["dangerText / dangerSoft", c.dangerText, c.dangerSoft, TEXT],
    ["warning / warningSoft", c.warning, c.warningSoft, TEXT],
    ["success / successSoft", c.success, c.successSoft, TEXT],
    ["warning / bgSurface", c.warning, c.bgSurface, TEXT],
    ["success / bgSurface", c.success, c.bgSurface, TEXT],
    ["statusRunning / bgSurface", c.statusRunning, c.bgSurface, UI],
    ["statusDone / bgSurface", c.statusDone, c.bgSurface, UI],
    ["statusFailed / bgSurface", c.statusFailed, c.bgSurface, UI],
    ["statusBlocked / bgSurface", c.statusBlocked, c.bgSurface, UI],
    ["statusUnknown / bgSurface", c.statusUnknown, c.bgSurface, UI],
    ["textPrimary / selection", c.textPrimary, selection, TEXT],
    ["textPrimary / material.chrome", c.textPrimary, chrome, TEXT],
    ["textSecondary / material.regular", c.textSecondary, regular, TEXT],
    ["textSecondary / material.thick", c.textSecondary, thick, TEXT],
    ["accent / material.tint", c.accent, tint, TEXT],
    ["sentText / sentBubble", chat.sentText, chat.sentBubble, TEXT],
    ["sentTimestamp / sentBubble", chat.sentTimestamp, chat.sentBubble, TEXT],
    ["receivedText / receivedBubble", chat.receivedText, chat.receivedBubble, TEXT],
    ["receivedTimestamp / receivedBubble", chat.receivedTimestamp, chat.receivedBubble, TEXT],
    ["receivedText / chat.background", chat.receivedText, chat.background, TEXT],
    ["link / receivedBubble", chat.link, chat.receivedBubble, TEXT],
    ["link / chat.background", chat.link, chat.background, TEXT],
    ["outboundSentClock / chat.background", chat.outboundSentClock, chat.background, UI],
    ["textPrimary / composerBackground", c.textPrimary, chat.composerBackground, TEXT],
    ["textTertiary / composerBackground", c.textTertiary, chat.composerBackground, TEXT],
  ];
}

const SCHEMES: ThemeColorScheme[] = ["light", "dark"];

describe("Zen palette WCAG AA contrast", () => {
  for (const colorScheme of SCHEMES) {
    for (const { id: accentId } of ZEN_ACCENTS) {
      test(`${colorScheme}/${accentId}: every shipped text and affordance pairing meets AA`, () => {
        const failures = pairings(resolveTheme({ colorScheme, accentId }))
          .map(([label, fg, bg, minimum]) => ({ label, ratio: contrastRatio(fg, bg), minimum }))
          .filter(({ ratio, minimum }) => ratio < minimum)
          .map(({ label, ratio, minimum }) => `${label}: ${ratio.toFixed(2)} < ${minimum}`);
        expect(failures).toEqual([]);
      });

      test(`${colorScheme}/${accentId}: the densest activity cell stays visible and ordered`, () => {
        const { dataVisualization, surfaces } = resolveTheme({ colorScheme, accentId });
        const ramp = dataVisualization.activityRamp;
        expect(contrastRatio(ramp[3], surfaces.card)).toBeGreaterThanOrEqual(UI);
        const distance = ramp.map((cell) => contrastRatio(cell, surfaces.card));
        for (let index = 1; index < distance.length; index += 1) {
          expect(distance[index]).toBeGreaterThan(distance[index - 1]);
        }
      });

      test(`${colorScheme}/${accentId}: running status stays apart from unknown, done and danger`, () => {
        const { colors } = resolveTheme({ colorScheme, accentId });
        const others = [colors.statusUnknown, colors.statusDone, colors.statusFailed, colors.statusBlocked];
        for (const other of others) {
          expect(oklabDistance(colors.statusRunning, other)).toBeGreaterThan(0.06);
        }
        expect(oklabDistance(colors.accent, colors.statusFailed)).toBeGreaterThan(0.06);
        expect(oklabDistance(colors.accent, colors.success)).toBeGreaterThan(0.06);
      });
    }

    test(`${colorScheme}: default accent resolves to sage`, () => {
      expect(resolveTheme({ colorScheme }).accentId).toBe("sage");
      expect(resolveTheme({ colorScheme, accentId: "unknown" }).accentId).toBe("sage");
    });

    test(`${colorScheme}: elevation levels are tonally distinct`, () => {
      const { colors } = resolveTheme({ colorScheme });
      const levels = [colors.bgPrimary, colors.bgSurface, colors.bgElevated, colors.surfacePressed];
      expect(new Set(levels).size).toBe(levels.length);
      // Adjacent levels need a perceptible step, not just a different hex.
      const luminances = levels.map(relativeLuminance);
      const minStep = colorScheme === "dark" ? 1.1 : 1.03;
      const ordered = [...luminances].sort((a, b) => a - b);
      for (let index = 1; index < ordered.length; index += 1) {
        expect((ordered[index] + 0.05) / (ordered[index - 1] + 0.05)).toBeGreaterThanOrEqual(minStep);
      }
    });
  }

  test("accent and success are distinct hues, not two greens", () => {
    for (const colorScheme of SCHEMES) {
      const { colors } = resolveTheme({ colorScheme });
      expect(colors.success).not.toBe(colors.accent);
      expect(colors.statusDone).not.toBe(colors.statusRunning);
    }
  });
});
