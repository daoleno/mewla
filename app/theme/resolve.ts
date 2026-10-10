import {
  DARK_APP_COLORS,
  DARK_CHAT_PALETTE,
  DARK_DATA_VISUALIZATION,
  DARK_MATERIALS,
  DARK_SURFACE_PALETTE,
  LIGHT_APP_COLORS,
  LIGHT_CHAT_PALETTE,
  LIGHT_DATA_VISUALIZATION,
  LIGHT_MATERIALS,
  LIGHT_SURFACE_PALETTE,
} from './primitives';
import { DEFAULT_THEME_IDS, getThemeById } from './registry';
import type { ResolvedTheme, ThemeColorScheme } from './types';

export function resolveTheme({
  colorScheme,
  themeId,
}: {
  colorScheme: ThemeColorScheme;
  themeId?: string | null;
}): ResolvedTheme {
  const fallbackId = DEFAULT_THEME_IDS[colorScheme];
  const requested = themeId ? getThemeById(themeId) : undefined;
  const definition = requested ?? getThemeById(fallbackId)!;
  const isLight = definition.colorScheme === 'light';

  return {
    ...definition,
    colors: isLight ? LIGHT_APP_COLORS : DARK_APP_COLORS,
    chat: isLight ? LIGHT_CHAT_PALETTE : DARK_CHAT_PALETTE,
    surfaces: isLight ? LIGHT_SURFACE_PALETTE : DARK_SURFACE_PALETTE,
    materials: isLight ? LIGHT_MATERIALS : DARK_MATERIALS,
    dataVisualization: isLight ? LIGHT_DATA_VISUALIZATION : DARK_DATA_VISUALIZATION,
    isLight,
  };
}
