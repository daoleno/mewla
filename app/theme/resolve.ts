import {
  buildDarkAppColors,
  buildDarkChatPalette,
  buildDarkDataVisualization,
  buildDarkMaterials,
  buildLightAppColors,
  buildLightChatPalette,
  buildLightDataVisualization,
  buildLightMaterials,
  buildSurfacePalette,
  DEFAULT_ACCENT_ID,
  getAccentById,
} from './primitives';
import { DEFAULT_THEME_IDS, getThemeById } from './registry';
import type { ResolvedZenTheme, ThemeColorScheme } from './types';

export function resolveTheme({
  colorScheme,
  themeId,
  accentId,
}: {
  colorScheme: ThemeColorScheme;
  themeId?: string | null;
  accentId?: string | null;
}): ResolvedZenTheme {
  const fallbackId = DEFAULT_THEME_IDS[colorScheme];
  const requested = themeId ? getThemeById(themeId) : undefined;
  const definition = requested ?? getThemeById(fallbackId)!;
  const isLight = definition.colorScheme === 'light';
  const accent = getAccentById(accentId) ?? getAccentById(DEFAULT_ACCENT_ID)!;
  const colors = isLight ? buildLightAppColors(accent) : buildDarkAppColors(accent);

  return {
    ...definition,
    colors,
    chat: isLight ? buildLightChatPalette(accent) : buildDarkChatPalette(accent),
    surfaces: buildSurfacePalette(colors),
    materials: isLight ? buildLightMaterials(accent) : buildDarkMaterials(accent),
    dataVisualization: isLight
      ? buildLightDataVisualization(accent)
      : buildDarkDataVisualization(accent),
    isLight,
    accentId: accent.id,
  };
}
