import type { ThemeDefinition } from '../types';
import {
  LIGHT_APP_COLORS,
  LIGHT_CHAT_PALETTE,
  LIGHT_MATERIALS,
  LIGHT_SURFACE_PALETTE,
} from '../primitives';
import { TELEGRAM_AVATAR_COLORS } from './shared';

/** Logo-derived light theme with a crisp neutral canvas and sage accents. */
export const classicLightTheme: ThemeDefinition = {
  id: 'classic-light',
  name: 'Classic Day',
  colorScheme: 'light',
  colors: LIGHT_APP_COLORS,
  chat: LIGHT_CHAT_PALETTE,
  surfaces: LIGHT_SURFACE_PALETTE,
  materials: LIGHT_MATERIALS,
  avatarColors: TELEGRAM_AVATAR_COLORS,
};
