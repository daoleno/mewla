import { describe, expect, test } from "bun:test";
import { parseHex, relativeLuminance, rgbToHex } from "./colorUtils";
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
    test(`${colorScheme}: every shipped text and affordance pairing meets AA`, () => {
      const failures = pairings(resolveTheme({ colorScheme }))
        .map(([label, fg, bg, minimum]) => ({ label, ratio: contrastRatio(fg, bg), minimum }))
        .filter(({ ratio, minimum }) => ratio < minimum)
        .map(({ label, ratio, minimum }) => `${label}: ${ratio.toFixed(2)} < ${minimum}`);
      expect(failures).toEqual([]);
    });

    test(`${colorScheme}: the densest activity cell stays visible on the card`, () => {
      const { dataVisualization, surfaces } = resolveTheme({ colorScheme });
      const ramp = dataVisualization.activityRamp;
      expect(contrastRatio(ramp[3], surfaces.card)).toBeGreaterThanOrEqual(UI);
      // Monotonic: each step moves away from the card, so intensity reads in order.
      const distance = ramp.map((cell) => contrastRatio(cell, surfaces.card));
      for (let index = 1; index < distance.length; index += 1) {
        expect(distance[index]).toBeGreaterThan(distance[index - 1]);
      }
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
