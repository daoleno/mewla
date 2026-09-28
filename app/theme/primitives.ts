import type { AppColors } from './palette';
import type {
  ChatPalette,
  DataVisualizationPalette,
  MaterialPalette,
  SurfacePalette,
} from './types';

const TRANSPARENT = 'transparent';

// Sage ramp tuned in OKLCH (hue ~156): chroma peaks through 400–600 so the
// accent reads alive, and the dark end stays green instead of turning grey mud.
export const ZEN_SAGE = {
  50: '#F1F9F3',
  100: '#DCF2E3',
  200: '#BEE6CC',
  300: '#9ED7B2',
  400: '#7DC398',
  500: '#559E72',
  600: '#397C55',
  700: '#2A5F41',
  800: '#214630',
  900: '#1A3123',
  950: '#0D1911',
} as const;

export const ZEN_BRAND_COLORS = {
  // Warm ink rather than blue-black, so sage sits on it without vibrating.
  environment: '#12120E',
  sage: ZEN_SAGE[400],
  ivory: '#F2EEE5',
} as const;

// Warm stone canvas with near-white paper cards, the Apple layering model.
export const ZEN_LIGHT_NEUTRALS = {
  canvas: '#F5F3EE',
  surface: '#FEFDFB',
  elevated: '#EBEAE4',
  pressed: '#E0E0D9',
  textPrimary: '#141712',
  textSecondary: '#4E514A',
  textTertiary: '#62645D',
  borderSubtle: '#E3E3DD',
  border: '#D4D5CD',
  borderStrong: '#797B74',
} as const;

// Elevation ladder with ~0.03–0.04 OKLCH lightness steps between levels, so
// canvas, cards and sheets separate without hairlines doing all the work.
export const ZEN_DARK_NEUTRALS = {
  surface: '#1C1D18',
  elevated: '#252721',
  subtle: '#181814',
  active: '#1C3125',
  pressed: '#2E302A',
  textPrimary: '#F3F0E9',
  textSecondary: '#C5C5BC',
  textTertiary: '#A2A49B',
  borderSubtle: '#2C2D28',
  border: '#393A34',
  borderStrong: '#7F8179',
  modalSurfaceAlt: '#292B25',
} as const;

// Success is teal, not a second sage: it stays apart from the accent and from
// danger/warning under deuteranopia and protanopia simulation.
export const ZEN_LIGHT_STATUS = {
  danger: '#AA1F1F',
  dangerSoft: '#FFE6E3',
  warning: '#9A5B00',
  warningSoft: '#FCEAD0',
  success: '#005E53',
  successSoft: '#DAF4EF',
} as const;

export const ZEN_DARK_STATUS = {
  danger: '#F07F77',
  dangerSoft: '#3A1D1B',
  warning: '#F6C16B',
  warningSoft: '#302412',
  success: '#80E7D6',
  successSoft: '#102B26',
} as const;

export const ZEN_LIGHT_OVERLAYS = {
  selection: 'rgba(57,124,85,0.22)',
  modalBackdrop: 'rgba(20,23,18,0.32)',
} as const;

export const ZEN_DARK_OVERLAYS = {
  selection: 'rgba(138,208,164,0.30)',
  modalBackdrop: 'rgba(0,0,0,0.6)',
} as const;

// Scheme accents sit between ramp stops: light needs 4.5:1 for white label
// text on it, dark needs chroma without glowing on the ink canvas.
const ZEN_LIGHT_ACCENT = '#2E6F4A';
const ZEN_LIGHT_ACCENT_STRONG = '#235A3B';
const ZEN_DARK_ACCENT = '#8AD0A4';
const ZEN_DARK_ACCENT_STRONG = '#B3E6C4';

export const ZEN_LIGHT_APP_COLORS: AppColors = {
  bgPrimary: ZEN_LIGHT_NEUTRALS.canvas,
  bgSurface: ZEN_LIGHT_NEUTRALS.surface,
  bgElevated: ZEN_LIGHT_NEUTRALS.elevated,
  textPrimary: ZEN_LIGHT_NEUTRALS.textPrimary,
  textSecondary: ZEN_LIGHT_NEUTRALS.textSecondary,
  textTertiary: ZEN_LIGHT_NEUTRALS.textTertiary,
  accent: ZEN_LIGHT_ACCENT,
  accentSoft: ZEN_SAGE[100],
  accentStrong: ZEN_LIGHT_ACCENT_STRONG,
  logoDetail: ZEN_SAGE[900],
  statusFailed: ZEN_LIGHT_STATUS.danger,
  statusBlocked: ZEN_LIGHT_STATUS.warning,
  statusUnknown: ZEN_LIGHT_NEUTRALS.textTertiary,
  statusRunning: ZEN_LIGHT_ACCENT,
  statusDone: ZEN_LIGHT_STATUS.success,
  zenGreen: ZEN_LIGHT_STATUS.success,
  priorityUrgent: ZEN_LIGHT_STATUS.danger,
  priorityHigh: ZEN_LIGHT_STATUS.warning,
  priorityMedium: ZEN_LIGHT_ACCENT,
  priorityLow: ZEN_LIGHT_NEUTRALS.textSecondary,
  border: ZEN_LIGHT_NEUTRALS.border,
  borderSubtle: ZEN_LIGHT_NEUTRALS.borderSubtle,
  borderStrong: ZEN_LIGHT_NEUTRALS.borderStrong,
  surfaceSubtle: ZEN_LIGHT_NEUTRALS.elevated,
  surfacePressed: ZEN_LIGHT_NEUTRALS.pressed,
  surfaceActive: ZEN_SAGE[100],
  inputBackground: ZEN_LIGHT_NEUTRALS.surface,
  disabledSurface: ZEN_LIGHT_NEUTRALS.pressed,
  modalBackdrop: ZEN_LIGHT_OVERLAYS.modalBackdrop,
  modalSurface: ZEN_LIGHT_NEUTRALS.surface,
  modalSurfaceAlt: ZEN_LIGHT_NEUTRALS.elevated,
  textOnAccent: ZEN_LIGHT_NEUTRALS.surface,
  focusRing: ZEN_LIGHT_ACCENT_STRONG,
  selectionBackground: ZEN_LIGHT_OVERLAYS.selection,
  promptGreen: ZEN_LIGHT_STATUS.success,
  promptYellow: ZEN_LIGHT_STATUS.warning,
  warning: ZEN_LIGHT_STATUS.warning,
  dangerText: ZEN_LIGHT_STATUS.danger,
  success: ZEN_LIGHT_STATUS.success,
  disabledText: ZEN_LIGHT_NEUTRALS.textTertiary,
  dangerSoft: ZEN_LIGHT_STATUS.dangerSoft,
  warningSoft: ZEN_LIGHT_STATUS.warningSoft,
  successSoft: ZEN_LIGHT_STATUS.successSoft,
  shadowColor: ZEN_BRAND_COLORS.environment,
};

export const ZEN_DARK_APP_COLORS: AppColors = {
  bgPrimary: ZEN_BRAND_COLORS.environment,
  bgSurface: ZEN_DARK_NEUTRALS.surface,
  bgElevated: ZEN_DARK_NEUTRALS.elevated,
  textPrimary: ZEN_DARK_NEUTRALS.textPrimary,
  textSecondary: ZEN_DARK_NEUTRALS.textSecondary,
  textTertiary: ZEN_DARK_NEUTRALS.textTertiary,
  accent: ZEN_DARK_ACCENT,
  accentSoft: ZEN_DARK_NEUTRALS.active,
  accentStrong: ZEN_DARK_ACCENT_STRONG,
  logoDetail: ZEN_BRAND_COLORS.ivory,
  statusFailed: ZEN_DARK_STATUS.danger,
  statusBlocked: ZEN_DARK_STATUS.warning,
  statusUnknown: ZEN_DARK_NEUTRALS.textTertiary,
  statusRunning: ZEN_DARK_ACCENT,
  statusDone: ZEN_DARK_STATUS.success,
  zenGreen: ZEN_DARK_STATUS.success,
  priorityUrgent: ZEN_DARK_STATUS.danger,
  priorityHigh: ZEN_DARK_STATUS.warning,
  priorityMedium: ZEN_DARK_ACCENT,
  priorityLow: ZEN_DARK_NEUTRALS.textSecondary,
  border: ZEN_DARK_NEUTRALS.border,
  borderSubtle: ZEN_DARK_NEUTRALS.borderSubtle,
  borderStrong: ZEN_DARK_NEUTRALS.borderStrong,
  surfaceSubtle: ZEN_DARK_NEUTRALS.subtle,
  surfacePressed: ZEN_DARK_NEUTRALS.pressed,
  surfaceActive: ZEN_DARK_NEUTRALS.active,
  inputBackground: ZEN_DARK_NEUTRALS.subtle,
  disabledSurface: ZEN_DARK_NEUTRALS.elevated,
  modalBackdrop: ZEN_DARK_OVERLAYS.modalBackdrop,
  modalSurface: ZEN_DARK_NEUTRALS.surface,
  modalSurfaceAlt: ZEN_DARK_NEUTRALS.modalSurfaceAlt,
  textOnAccent: ZEN_BRAND_COLORS.environment,
  focusRing: ZEN_DARK_ACCENT_STRONG,
  selectionBackground: ZEN_DARK_OVERLAYS.selection,
  promptGreen: ZEN_DARK_STATUS.success,
  promptYellow: ZEN_DARK_STATUS.warning,
  warning: ZEN_DARK_STATUS.warning,
  dangerText: ZEN_DARK_STATUS.danger,
  success: ZEN_DARK_STATUS.success,
  disabledText: ZEN_DARK_NEUTRALS.textTertiary,
  dangerSoft: ZEN_DARK_STATUS.dangerSoft,
  warningSoft: ZEN_DARK_STATUS.warningSoft,
  successSoft: ZEN_DARK_STATUS.successSoft,
  shadowColor: '#000000',
};

export const ZEN_LIGHT_CHAT_PALETTE: ChatPalette = {
  layout: 'telegram',
  showWallpaper: false,
  showTimestamps: false,
  showDateDividers: true,
  background: ZEN_LIGHT_NEUTRALS.canvas,
  sentBubble: ZEN_SAGE[200],
  receivedBubble: ZEN_LIGHT_NEUTRALS.surface,
  sentText: ZEN_LIGHT_NEUTRALS.textPrimary,
  receivedText: ZEN_LIGHT_NEUTRALS.textPrimary,
  sentTimestamp: ZEN_LIGHT_NEUTRALS.textSecondary,
  receivedTimestamp: ZEN_LIGHT_NEUTRALS.textTertiary,
  // Outside the bubble on chat.background — high-contrast sage, not outline chrome.
  outboundSentClock: ZEN_SAGE[700],
  composerBackground: ZEN_LIGHT_NEUTRALS.surface,
  composerBorder: ZEN_LIGHT_NEUTRALS.border,
  composerDock: TRANSPARENT,
  link: ZEN_LIGHT_ACCENT_STRONG,
  patternIcon: TRANSPARENT,
};

export const ZEN_DARK_CHAT_PALETTE: ChatPalette = {
  layout: 'telegram',
  showWallpaper: false,
  showTimestamps: false,
  showDateDividers: true,
  background: ZEN_BRAND_COLORS.environment,
  // Deep saturated sage on warm ink reads as a lit panel, not a murky mass.
  sentBubble: ZEN_SAGE[700],
  receivedBubble: ZEN_DARK_NEUTRALS.surface,
  sentText: ZEN_DARK_NEUTRALS.textPrimary,
  receivedText: ZEN_DARK_NEUTRALS.textPrimary,
  sentTimestamp: ZEN_SAGE[200],
  receivedTimestamp: ZEN_DARK_NEUTRALS.textTertiary,
  // Outside the bubble on the ink canvas — bright sage for status readability.
  outboundSentClock: ZEN_SAGE[200],
  composerBackground: ZEN_DARK_NEUTRALS.surface,
  composerBorder: ZEN_DARK_NEUTRALS.border,
  composerDock: TRANSPARENT,
  link: ZEN_DARK_ACCENT_STRONG,
  patternIcon: TRANSPARENT,
};

export const ZEN_LIGHT_SURFACE_PALETTE: SurfacePalette = {
  card: ZEN_LIGHT_APP_COLORS.bgSurface,
  cardStrong: ZEN_LIGHT_APP_COLORS.bgElevated,
  subtle: ZEN_LIGHT_APP_COLORS.surfaceSubtle,
  border: ZEN_LIGHT_APP_COLORS.border,
  sectionLabel: ZEN_LIGHT_APP_COLORS.textTertiary,
};

export const ZEN_DARK_SURFACE_PALETTE: SurfacePalette = {
  card: ZEN_DARK_APP_COLORS.bgSurface,
  cardStrong: ZEN_DARK_APP_COLORS.bgElevated,
  subtle: ZEN_DARK_APP_COLORS.surfaceSubtle,
  border: ZEN_DARK_APP_COLORS.border,
  sectionLabel: ZEN_DARK_APP_COLORS.textTertiary,
};

export const ZEN_LIGHT_MATERIALS: MaterialPalette = {
  chrome: 'rgba(245,243,238,0.88)',
  regular: 'rgba(254,253,251,0.92)',
  thick: 'rgba(254,253,251,0.97)',
  thin: 'rgba(254,253,251,0.68)',
  highlight: 'rgba(255,255,255,0.95)',
  stroke: 'rgba(20,23,18,0.08)',
  separator: 'rgba(20,23,18,0.11)',
  tint: 'rgba(57,124,85,0.12)',
};

export const ZEN_DARK_MATERIALS: MaterialPalette = {
  chrome: 'rgba(18,18,14,0.86)',
  regular: 'rgba(34,35,30,0.92)',
  thick: 'rgba(41,43,37,0.97)',
  thin: 'rgba(50,52,45,0.64)',
  // Warm-white light so the lit edge matches the ivory text, not a blue glint.
  highlight: 'rgba(255,250,235,0.10)',
  stroke: 'rgba(255,250,235,0.08)',
  separator: 'rgba(255,250,235,0.10)',
  tint: 'rgba(138,208,164,0.18)',
};

export const ZEN_LIGHT_DATA_VISUALIZATION: DataVisualizationPalette = {
  activityRamp: [ZEN_SAGE[100], ZEN_SAGE[300], ZEN_SAGE[500], ZEN_SAGE[700]],
};

export const ZEN_DARK_DATA_VISUALIZATION: DataVisualizationPalette = {
  activityRamp: [
    ZEN_SAGE[900],
    ZEN_SAGE[700],
    ZEN_SAGE[500],
    ZEN_SAGE[300],
  ],
};
