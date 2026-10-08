import type { ThemeDefinition } from '../types';
import {
  DARK_APP_COLORS,
  DARK_CHAT_PALETTE,
  DARK_DATA_VISUALIZATION,
  DARK_MATERIALS,
  DARK_SURFACE_PALETTE,
} from '../primitives';
import { TELEGRAM_AVATAR_COLORS } from './shared';

/** Logo-derived dark theme with solid graphite surfaces and sage accents. */
export const classicDarkTheme: ThemeDefinition = {
  id: 'classic-dark',
  name: 'Classic Night',
  colorScheme: 'dark',
  colors: DARK_APP_COLORS,
  chat: DARK_CHAT_PALETTE,
  surfaces: DARK_SURFACE_PALETTE,
  materials: DARK_MATERIALS,
  dataVisualization: DARK_DATA_VISUALIZATION,
  avatarColors: TELEGRAM_AVATAR_COLORS,
};
