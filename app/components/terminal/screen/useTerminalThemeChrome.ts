import { useMemo } from "react";
import {
  buildTerminalChrome,
  isLightTerminalTheme,
  resolveTerminalTheme,
  resolveTerminalThemeName,
} from "../../../constants/terminalThemes";
import { useAppTheme } from "../../../constants/tokens";
import { buildChatChrome } from "../../../theme";

export function useTerminalThemeChrome() {
  const { theme: appTheme } = useAppTheme();
  const themeName = useMemo(
    () => resolveTerminalThemeName(appTheme.colorScheme),
    [appTheme.colorScheme],
  );
  const terminalTheme = useMemo(
    () => resolveTerminalTheme(themeName),
    [themeName],
  );
  const chromeColors = useMemo(
    () => ({
      ...buildTerminalChrome(terminalTheme),
      appBackground: appTheme.colors.bgPrimary,
      surface: appTheme.colors.modalSurface,
      surfaceMuted: appTheme.colors.modalSurfaceAlt,
      surfaceActive: appTheme.colors.surfaceActive,
      composerInput: appTheme.colors.inputBackground,
      border: appTheme.colors.border,
      borderStrong: appTheme.colors.borderStrong,
      text: appTheme.colors.textPrimary,
      textMuted: appTheme.colors.textSecondary,
      textSubtle: appTheme.colors.textTertiary,
      textOnAccent: appTheme.colors.textOnAccent,
      accent: appTheme.colors.accent,
      accentSoft: appTheme.colors.accentSoft,
      disabledSurface: appTheme.colors.disabledSurface,
      focus: appTheme.colors.focusRing,
      link: appTheme.colors.accentStrong,
      danger: appTheme.colors.dangerText,
      dangerSoft: appTheme.colors.dangerSoft,
      overlay: appTheme.colors.modalBackdrop,
      shadowColor: appTheme.colors.shadowColor,
    }),
    [terminalTheme, appTheme.colors],
  );
  const chat = useMemo(() => buildChatChrome(appTheme), [appTheme]);
  const statusBarStyle: "dark" | "light" = isLightTerminalTheme(terminalTheme)
    ? "dark"
    : "light";

  return {
    chromeColors,
    chatChrome: chat.chrome,
    chatTheme: chat.theme,
    statusBarStyle,
    terminalTheme,
  };
}
