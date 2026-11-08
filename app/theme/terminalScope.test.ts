import { describe, expect, test } from "bun:test";
import { buildTerminalChrome, TerminalThemes } from "../constants/terminalThemes";
import { resolveTheme } from "./resolve";
import { terminalScopedTheme } from "./terminalScope";

describe("terminal theme scope", () => {
  const base = resolveTheme({ colorScheme: "light" });
  for (const [name, theme] of Object.entries(TerminalThemes)) {
    test(`${name}: shared tokens carry the terminal colours`, () => {
      const chrome = buildTerminalChrome(theme);
      const scoped = terminalScopedTheme(base, chrome);
      expect(scoped.colors.bgSurface).toBe(chrome.surface);
      expect(scoped.colors.textPrimary).toBe(chrome.text);
      expect(scoped.colors.accent).toBe(chrome.accent);
      expect(scoped.colors.dangerText).toBe(chrome.danger);
      expect(scoped.colors.inputBackground).toBe(chrome.composerInput);
      expect(scoped.materials.tint).toBe(chrome.accentSoft);
      expect(scoped.isLight).toBe(name === "light");
      // Everything the kit reads still exists.
      for (const key of Object.keys(base.colors)) {
        expect(typeof (scoped.colors as unknown as Record<string, unknown>)[key]).toBe("string");
      }
    });
  }
});
