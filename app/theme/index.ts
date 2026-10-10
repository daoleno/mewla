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

export { ThemeProvider, useThemeContext } from './provider';
export { buildChatChrome } from './buildChatChrome';
export { resolveTheme } from './resolve';
