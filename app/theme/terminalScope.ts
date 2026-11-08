import type { TerminalThemeChrome } from "../constants/terminalThemes";
import { relativeLuminance } from "./colorUtils";
import type { ResolvedTheme } from "./types";

/**
 * The app theme as seen from inside a terminal surface: the same tokens the
 * shared Button, IconButton, InlineNotice, ListRow, input and pills read, but
 * carrying the terminal theme's colours. A terminal surface is wrapped in
 * `TerminalThemeScope` once, and the kit just works inside it.
 */
export function terminalScopedTheme(base: ResolvedTheme, chrome: TerminalThemeChrome): ResolvedTheme {
  const isLight = relativeLuminance(chrome.appBackground) > 0.4;
  return {
    ...base,
    colorScheme: isLight ? "light" : "dark",
    isLight,
    colors: {
      ...base.colors,
      bgPrimary: chrome.appBackground,
      bgSurface: chrome.surface,
      bgElevated: chrome.surface,
      textPrimary: chrome.text,
      textSecondary: chrome.textMuted,
      textTertiary: chrome.textSubtle,
      textOnAccent: chrome.textOnAccent,
      accent: chrome.accent,
      accentSoft: chrome.accentSoft,
      accentStrong: chrome.link,
      seal: chrome.seal,
      sealText: chrome.sealText,
      onSeal: chrome.onSeal,
      statusFailed: chrome.danger,
      statusBlocked: chrome.statusBlocked,
      statusWarning: chrome.statusWarning,
      statusRunning: chrome.statusRunning,
      statusDone: chrome.statusReady,
      border: chrome.border,
      borderSubtle: chrome.border,
      borderStrong: chrome.borderStrong,
      surfaceSubtle: chrome.surfaceMuted,
      surfacePressed: chrome.surfaceActive,
      surfaceActive: chrome.surfaceActive,
      inputBackground: chrome.composerInput,
      disabledSurface: chrome.disabledSurface,
      disabledText: chrome.textSubtle,
      modalBackdrop: chrome.overlay,
      modalSurface: chrome.surface,
      modalSurfaceAlt: chrome.surfaceMuted,
      focusRing: chrome.focus,
      selectionBackground: chrome.accentSoft,
      warning: chrome.statusWarning,
      dangerText: chrome.danger,
      success: chrome.statusReady,
      dangerSoft: chrome.dangerSoft,
      shadowColor: chrome.shadowColor,
    },
    materials: {
      ...base.materials,
      chrome: chrome.surface,
      regular: chrome.surface,
      thick: chrome.surface,
      thin: chrome.surfaceMuted,
      stroke: chrome.border,
      separator: chrome.border,
      tint: chrome.accentSoft,
    },
  };
}
