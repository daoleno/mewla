import {
  resolveTerminalTheme,
  type TerminalThemeChrome,
  type TerminalThemePalette,
} from '../constants/terminalThemes';
import type { ResolvedTheme } from './types';

export function buildChatChrome(theme: ResolvedTheme): {
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
} {
  const baseTheme = resolveTerminalTheme(theme.isLight ? 'light' : 'dark');
  const { colors, chat } = theme;

  const chrome: TerminalThemeChrome = {
    appBackground: chat.background,
    surface: chat.receivedBubble,
    // A quiet fill (code, tables, wells): the landing's tint, never the
    // sent bubble, which is ink in the light theme.
    surfaceMuted: colors.bgElevated,
    surfaceActive:
      chat.composerDock === 'transparent' ? chat.composerBackground : chat.composerDock,
    composerInput: chat.composerBackground,
    border: chat.composerBorder,
    borderStrong: colors.borderStrong,
    text: chat.receivedText,
    textMuted: colors.textSecondary,
    textSubtle: colors.textTertiary,
    textOnAccent: colors.textOnAccent,
    accent: colors.accent,
    accentSoft: colors.accentSoft,
    disabledSurface: colors.disabledSurface,
    focus: colors.focusRing,
    link: chat.link,
    danger: colors.dangerText,
    dangerSoft: colors.dangerSoft,
    seal: colors.seal,
    sealText: colors.sealText,
    onSeal: colors.onSeal,
    statusReady: colors.statusDone,
    statusRunning: colors.statusRunning,
    statusWarning: colors.statusWarning,
    statusBlocked: colors.statusBlocked,
    overlay: colors.modalBackdrop,
    shadowColor: colors.shadowColor,
  };

  const palette: TerminalThemePalette = {
    ...baseTheme,
    background: chat.background,
    foreground: chat.receivedText,
    cursor: colors.accent,
    cursorAccent: theme.isLight ? colors.textOnAccent : chat.background,
    selectionBackground: colors.selectionBackground,
  };

  return { chrome, theme: palette };
}
