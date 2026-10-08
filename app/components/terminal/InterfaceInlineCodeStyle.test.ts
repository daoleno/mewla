// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { buildChatChrome } from "../../theme/buildChatChrome";
import { resolveTheme } from "../../theme/resolve";
import { interfaceInlineCodeStyle } from "./InterfaceInlineCodeStyle";

describe("interfaceInlineCodeStyle", () => {
  for (const colorScheme of ["dark", "light"] as const) {
    test(`${colorScheme}: long wrapped inline code stays typographic without opaque tiles`, () => {
      const appTheme = resolveTheme({ colorScheme });
      const { chrome, theme } = buildChatChrome(appTheme);
      const style = interfaceInlineCodeStyle(theme, false);

      expect(style.color).toBe(theme.cyan);
      expect(style.backgroundColor).toBe("transparent");
      expect(style.borderColor).toBe("transparent");
      expect(style.fontFamily).toBe("MapleMono-CN-Regular");

      // surfaceMuted is the quiet tint (never the ink sent bubble), and inline
      // code must not paint it as a tile either.
      expect(chrome.surfaceMuted).toBe(appTheme.colors.bgElevated);
      expect(chrome.surfaceMuted).not.toBe(appTheme.chat.sentBubble);
      expect(style.backgroundColor).not.toBe(chrome.surfaceMuted);
    });

    test(`${colorScheme}: compact sizing keeps the transparent surface contract`, () => {
      const { theme } = buildChatChrome(resolveTheme({ colorScheme }));
      const style = interfaceInlineCodeStyle(theme, true);
      expect(style.fontSize).toBe(12);
      expect(style.lineHeight).toBe(18);
      expect(style.backgroundColor).toBe("transparent");
      expect(style.borderColor).toBe("transparent");
    });
  }
});
