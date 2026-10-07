import { describe, expect, test } from "bun:test";
import { parseHex, relativeLuminance } from "../theme/colorUtils";
import { buildChatChrome } from "../theme/buildChatChrome";
import { resolveTheme } from "../theme/resolve";
import type { ThemeColorScheme } from "../theme/types";
import {
  buildTerminalChrome,
  TerminalThemes,
  type TerminalThemePalette,
} from "./terminalThemes";

// WCAG AA: 4.5:1 for text, 3:1 for bright accents and the cursor block.
const TEXT = 4.5;
const UI = 3;

function contrastRatio(foreground: string, background: string): number {
  const a = relativeLuminance(foreground);
  const b = relativeLuminance(background);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
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

function oklabDistance(a: string, b: string): number {
  const [l1, a1, b1] = toOklab(a);
  const [l2, a2, b2] = toOklab(b);
  return Math.hypot(l1 - l2, a1 - a2, b1 - b2);
}

type AnsiKey = Exclude<
  keyof TerminalThemePalette,
  | "background"
  | "foreground"
  | "cursor"
  | "cursorAccent"
  | "selectionBackground"
  | "selectionInactiveBackground"
>;

const NORMAL: AnsiKey[] = ["red", "green", "yellow", "blue", "magenta", "cyan"];
const BRIGHT: AnsiKey[] = [
  "brightBlack",
  "brightRed",
  "brightGreen",
  "brightYellow",
  "brightBlue",
  "brightMagenta",
  "brightCyan",
];

const SCHEMES: ThemeColorScheme[] = ["light", "dark"];

describe("terminal palette on paper and ink", () => {
  for (const scheme of SCHEMES) {
    const palette = TerminalThemes[scheme];
    const app = resolveTheme({ colorScheme: scheme, accentId: "ink" });

    test(`${scheme}: the grid sits on the app canvas with an ink cursor`, () => {
      expect(palette.background).toBe(app.colors.bgPrimary);
      expect(palette.cursor).toBe(app.colors.accent);
      expect(contrastRatio(palette.foreground, palette.background)).toBeGreaterThanOrEqual(7);
      expect(contrastRatio(palette.cursor, palette.background)).toBeGreaterThanOrEqual(UI);
      expect(contrastRatio(palette.cursorAccent, palette.cursor)).toBeGreaterThanOrEqual(TEXT);
    });

    test(`${scheme}: ANSI colours stay legible`, () => {
      // Dark "black" is a background colour by convention; light keeps it
      // and "white" (a stone grey) as text.
      const normal: AnsiKey[] =
        scheme === "light" ? [...NORMAL, "black", "white"] : [...NORMAL, "white"];
      const bright: AnsiKey[] = scheme === "light" ? [...BRIGHT, "brightWhite"] : BRIGHT;
      const failures = [
        ...normal.map((key) => [key, contrastRatio(palette[key], palette.background), TEXT] as const),
        ...bright.map((key) => [key, contrastRatio(palette[key], palette.background), UI] as const),
      ].filter(([, ratio, minimum]) => ratio < minimum);
      expect(failures).toEqual([]);
    });

    test(`${scheme}: chat inline code and diff ink read on their wells`, () => {
      const { theme } = buildChatChrome(app);
      for (const surface of [app.colors.bgElevated, app.colors.bgSurface, app.chat.background]) {
        for (const key of ["cyan", "green", "red", "yellow", "blue", "magenta"] as const) {
          expect(contrastRatio(theme[key], surface)).toBeGreaterThanOrEqual(TEXT);
        }
      }
    });

    test(`${scheme}: terminal chrome keeps the app's seal and Work states`, () => {
      const chrome = buildTerminalChrome(palette);
      expect(chrome.seal).toBe(app.colors.seal);
      expect(chrome.sealText).toBe(app.colors.sealText);
      expect(chrome.onSeal).toBe(app.colors.onSeal);
      expect(chrome.danger).toBe(app.colors.statusFailed);
      expect(chrome.statusReady).toBe(app.colors.statusDone);
      expect(chrome.statusRunning).toBe(app.colors.statusRunning);
      expect(chrome.statusWarning).toBe(app.colors.statusWarning);
      expect(chrome.statusBlocked).toBe(app.colors.statusBlocked);
    });

    test(`${scheme}: ANSI red is never the seal`, () => {
      for (const key of ["red", "brightRed"] as const) {
        expect(oklabDistance(palette[key], app.colors.seal)).toBeGreaterThan(0.075);
        expect(oklabDistance(palette[key], app.colors.sealText)).toBeGreaterThan(0.075);
      }
    });
  }
});
