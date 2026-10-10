import { classicDarkTheme } from './definitions/classicDark';
import { classicLightTheme } from './definitions/classicLight';
import type { ThemeColorScheme, ThemeDefinition } from './types';

const THEME_REGISTRY: readonly ThemeDefinition[] = [
  classicDarkTheme,
  classicLightTheme,
] as const;

export const DEFAULT_THEME_IDS: Record<ThemeColorScheme, string> = {
  dark: classicDarkTheme.id,
  light: classicLightTheme.id,
};

export function getThemeById(id: string): ThemeDefinition | undefined {
  return THEME_REGISTRY.find((theme) => theme.id === id);
}
