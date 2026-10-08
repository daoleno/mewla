export type { AppColors } from './palette';
export type {
  AppPalette,
  ChatLayout,
  ChatPalette,
  DataVisualizationPalette,
  MaterialPalette,
  ResolvedTheme,
  SurfacePalette,
  ThemeColorScheme,
  ThemePreference,
  ThemeDefinition,
} from './types';

export {
  BRAND_COLORS,
  DARK_APP_COLORS,
  DARK_CHAT_PALETTE,
  DARK_DATA_VISUALIZATION,
  DARK_MATERIALS,
  DARK_NEUTRALS,
  DARK_OVERLAYS,
  DARK_STATUS,
  DARK_SURFACE_PALETTE,
  LIGHT_APP_COLORS,
  LIGHT_CHAT_PALETTE,
  LIGHT_DATA_VISUALIZATION,
  LIGHT_MATERIALS,
  LIGHT_NEUTRALS,
  LIGHT_OVERLAYS,
  LIGHT_STATUS,
  LIGHT_SURFACE_PALETTE,
  SAGE,
  THEME_ACCENTS,
  DEFAULT_ACCENT_ID,
  getAccentById,
} from './primitives';
export type { ThemeAccent, ThemeAccentId } from './primitives';

export { ThemeProvider, useThemeContext } from './provider';
export { buildChatChrome } from './buildChatChrome';
export { resolveTheme } from './resolve';
export {
  DEFAULT_THEME_IDS,
  getThemeById,
  listThemesForScheme,
  THEME_REGISTRY,
} from './registry';
export { classicDarkTheme } from './definitions/classicDark';
export { classicLightTheme } from './definitions/classicLight';
export { mixHex, relativeLuminance } from './colorUtils';
