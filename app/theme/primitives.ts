import type { AppColors } from './palette';
import type {
  ChatPalette,
  DataVisualizationPalette,
  MaterialPalette,
  SurfacePalette,
} from './types';

const TRANSPARENT = 'transparent';

// Sage ramp from the Zen mark; it now only colours the legacy logo (see
// ZEN_BRAND_COLORS.sage) and goes with the rename.
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

export type AccentRamp = { readonly [Stop in keyof typeof ZEN_SAGE]: string };

// Seal vermilion (hue ~28), the Mewla accent: 600 is the landing's seal ink,
// 700 its pressed/ink-on-tint shade, 100 the landing's red-soft wash.
export const ZEN_VERMILION: AccentRamp = {
  50: '#FDF4F1',
  100: '#FBEBE6',
  200: '#F6D3CA',
  300: '#EFB3A6',
  400: '#E8806F',
  500: '#D75444',
  600: '#C8372B',
  700: '#A92B21',
  800: '#7F2219',
  900: '#561A14',
  950: '#2E0F0B',
};

/**
 * Scheme accents sit between ramp stops: light needs 4.5:1 for white label
 * text on it, dark needs chroma without glowing on the ink canvas. `status`
 * paints running/in-progress marks and is never the accent.
 */
export interface AccentScheme {
  accent: string;
  accentStrong: string;
  /** Dark only: selected-row fill on the ink canvas. */
  active: string;
  status: string;
}

export interface ZenAccent {
  id: ZenAccentId;
  name: string;
  ramp: AccentRamp;
  light: AccentScheme;
  dark: AccentScheme;
}

/**
 * Vermilion is the one accent. The earlier sage/ink/clay/stone picker is gone;
 * a stored legacy preference resolves to the default.
 */
export type ZenAccentId = 'vermilion';

export const ZEN_ACCENTS: readonly ZenAccent[] = [
  {
    id: 'vermilion',
    name: 'Vermilion',
    ramp: ZEN_VERMILION,
    // Text-on-paper accent is a hair deeper than the seal (#C8372B) so it
    // holds 4.5:1 on pressed paper; the difference is below a JND (ΔE 0.026).
    // Running work borrows the landing's "run" blue: vermilion is the one
    // accent and must never be mistaken for a status.
    light: { accent: '#BC3328', accentStrong: '#A92B21', active: ZEN_VERMILION[100], status: '#2C55C0' },
    dark: { accent: '#FF8F80', accentStrong: '#FFB3A8', active: '#3A1F1B', status: '#8FB0FF' },
  },
];

export const DEFAULT_ACCENT_ID: ZenAccentId = 'vermilion';

export function getAccentById(id: string | null | undefined): ZenAccent | undefined {
  return ZEN_ACCENTS.find((accent) => accent.id === id);
}

const DEFAULT_ACCENT = getAccentById(DEFAULT_ACCENT_ID)!;

function rgba(hex: string, alpha: number): string {
  const n = hex.replace('#', '');
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(n.slice(i, i + 2), 16));
  return `rgba(${r},${g},${b},${alpha})`;
}

export const ZEN_BRAND_COLORS = {
  // The landing's dark sections: warm ink, never blue-black.
  environment: '#141210',
  sage: ZEN_SAGE[400],
  ivory: '#F2EEE5',
  /** The seal: the one brand red. */
  vermilion: ZEN_VERMILION[600],
  /** The cat's paper, carved out of the seal. */
  sealPaper: '#FBF6EC',
} as const;

// Landing paper and ink: warm white canvas, pure white cards, the landing's
// tint for raised fills and its hairline for pressed wells.
export const ZEN_LIGHT_NEUTRALS = {
  canvas: '#FBFAF7',
  surface: '#FFFFFF',
  elevated: '#F5F3EE',
  pressed: '#ECE8DF',
  textPrimary: '#161412',
  textSecondary: '#57514A',
  textTertiary: '#6C665D',
  borderSubtle: '#EBE6DC',
  border: '#DDD6C9',
  borderStrong: '#857E73',
} as const;

// The landing's dark sections (#141210) with a warm elevation ladder; each
// step is >= 1.1 luminance ratio so cards and sheets separate without lines.
export const ZEN_DARK_NEUTRALS = {
  surface: '#201D19',
  elevated: '#2C2823',
  subtle: '#1A1714',
  pressed: '#3A3530',
  textPrimary: '#F4F0EA',
  textSecondary: '#C9C1B6',
  textTertiary: '#A9A196',
  borderSubtle: '#2A2622',
  border: '#3F3933',
  borderStrong: '#8A8378',
  modalSurfaceAlt: '#2B2722',
} as const;

// The landing's status chips: done green, waiting amber. Failure is a rose
// crimson, held apart from the vermilion accent (OKLab ΔE > 0.1) and always
// shipped with its glyph and label, so it never relies on hue alone.
export const ZEN_LIGHT_STATUS = {
  danger: '#A3194F',
  dangerSoft: '#FBE4EC',
  warning: '#94600A',
  warningSoft: '#FDF1DC',
  success: '#22703C',
  successSoft: '#E3F4E8',
} as const;

export const ZEN_DARK_STATUS = {
  danger: '#F584C0',
  dangerSoft: '#3A1A2A',
  warning: '#F6C16B',
  warningSoft: '#302412',
  success: '#7BD394',
  successSoft: '#12291A',
} as const;

export const ZEN_LIGHT_OVERLAYS = {
  modalBackdrop: 'rgba(22,20,18,0.32)',
} as const;

export const ZEN_DARK_OVERLAYS = {
  modalBackdrop: 'rgba(0,0,0,0.6)',
} as const;

export function buildLightAppColors({ ramp, light: a }: ZenAccent): AppColors {
  return {
    bgPrimary: ZEN_LIGHT_NEUTRALS.canvas,
    bgSurface: ZEN_LIGHT_NEUTRALS.surface,
    bgElevated: ZEN_LIGHT_NEUTRALS.elevated,
    textPrimary: ZEN_LIGHT_NEUTRALS.textPrimary,
    textSecondary: ZEN_LIGHT_NEUTRALS.textSecondary,
    textTertiary: ZEN_LIGHT_NEUTRALS.textTertiary,
    accent: a.accent,
    accentSoft: a.active,
    accentStrong: a.accentStrong,
    // The Zen mark keeps its sage-ink ribbon until the rename replaces it.
    logoDetail: ZEN_SAGE[900],
    statusFailed: ZEN_LIGHT_STATUS.danger,
    statusBlocked: ZEN_LIGHT_STATUS.warning,
    statusUnknown: ZEN_LIGHT_NEUTRALS.textTertiary,
    statusRunning: a.status,
    statusDone: ZEN_LIGHT_STATUS.success,
    zenGreen: ZEN_LIGHT_STATUS.success,
    priorityUrgent: ZEN_LIGHT_STATUS.danger,
    priorityHigh: ZEN_LIGHT_STATUS.warning,
    priorityMedium: a.status,
    priorityLow: ZEN_LIGHT_NEUTRALS.textSecondary,
    border: ZEN_LIGHT_NEUTRALS.border,
    borderSubtle: ZEN_LIGHT_NEUTRALS.borderSubtle,
    borderStrong: ZEN_LIGHT_NEUTRALS.borderStrong,
    surfaceSubtle: ZEN_LIGHT_NEUTRALS.elevated,
    surfacePressed: ZEN_LIGHT_NEUTRALS.pressed,
    surfaceActive: a.active,
    inputBackground: ZEN_LIGHT_NEUTRALS.surface,
    disabledSurface: ZEN_LIGHT_NEUTRALS.pressed,
    modalBackdrop: ZEN_LIGHT_OVERLAYS.modalBackdrop,
    modalSurface: ZEN_LIGHT_NEUTRALS.surface,
    modalSurfaceAlt: ZEN_LIGHT_NEUTRALS.elevated,
    textOnAccent: ZEN_LIGHT_NEUTRALS.surface,
    focusRing: a.accentStrong,
    selectionBackground: rgba(ramp[600], 0.22),
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
}

export const ZEN_LIGHT_APP_COLORS: AppColors = buildLightAppColors(DEFAULT_ACCENT);

export function buildDarkAppColors({ dark: a }: ZenAccent): AppColors {
  return {
    bgPrimary: ZEN_BRAND_COLORS.environment,
    bgSurface: ZEN_DARK_NEUTRALS.surface,
    bgElevated: ZEN_DARK_NEUTRALS.elevated,
    textPrimary: ZEN_DARK_NEUTRALS.textPrimary,
    textSecondary: ZEN_DARK_NEUTRALS.textSecondary,
    textTertiary: ZEN_DARK_NEUTRALS.textTertiary,
    accent: a.accent,
    accentSoft: a.active,
    accentStrong: a.accentStrong,
    logoDetail: ZEN_BRAND_COLORS.ivory,
    statusFailed: ZEN_DARK_STATUS.danger,
    statusBlocked: ZEN_DARK_STATUS.warning,
    statusUnknown: ZEN_DARK_NEUTRALS.textTertiary,
    statusRunning: a.status,
    statusDone: ZEN_DARK_STATUS.success,
    zenGreen: ZEN_DARK_STATUS.success,
    priorityUrgent: ZEN_DARK_STATUS.danger,
    priorityHigh: ZEN_DARK_STATUS.warning,
    priorityMedium: a.status,
    priorityLow: ZEN_DARK_NEUTRALS.textSecondary,
    border: ZEN_DARK_NEUTRALS.border,
    borderSubtle: ZEN_DARK_NEUTRALS.borderSubtle,
    borderStrong: ZEN_DARK_NEUTRALS.borderStrong,
    surfaceSubtle: ZEN_DARK_NEUTRALS.subtle,
    surfacePressed: ZEN_DARK_NEUTRALS.pressed,
    surfaceActive: a.active,
    inputBackground: ZEN_DARK_NEUTRALS.subtle,
    disabledSurface: ZEN_DARK_NEUTRALS.elevated,
    modalBackdrop: ZEN_DARK_OVERLAYS.modalBackdrop,
    modalSurface: ZEN_DARK_NEUTRALS.surface,
    modalSurfaceAlt: ZEN_DARK_NEUTRALS.modalSurfaceAlt,
    textOnAccent: ZEN_BRAND_COLORS.environment,
    focusRing: a.accentStrong,
    selectionBackground: rgba(a.accent, 0.3),
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
}

export const ZEN_DARK_APP_COLORS: AppColors = buildDarkAppColors(DEFAULT_ACCENT);

export function buildLightChatPalette({ light: a }: ZenAccent): ChatPalette {
  return {
    layout: 'telegram',
    showWallpaper: false,
    showTimestamps: false,
    showDateDividers: true,
    background: ZEN_LIGHT_NEUTRALS.canvas,
    // The landing's chat: your words in an ink bubble, Brain's on the paper.
    sentBubble: ZEN_LIGHT_NEUTRALS.textPrimary,
    receivedBubble: ZEN_LIGHT_NEUTRALS.surface,
    sentText: '#F6F2EA',
    receivedText: ZEN_LIGHT_NEUTRALS.textPrimary,
    sentTimestamp: '#B8B0A5',
    receivedTimestamp: ZEN_LIGHT_NEUTRALS.textTertiary,
    // Outside the bubble on chat.background: quiet ink, not outline chrome.
    outboundSentClock: ZEN_LIGHT_NEUTRALS.textSecondary,
    composerBackground: ZEN_LIGHT_NEUTRALS.surface,
    composerBorder: ZEN_LIGHT_NEUTRALS.borderSubtle,
    composerDock: TRANSPARENT,
    link: a.accentStrong,
    patternIcon: TRANSPARENT,
  };
}

export const ZEN_LIGHT_CHAT_PALETTE: ChatPalette = buildLightChatPalette(DEFAULT_ACCENT);

export function buildDarkChatPalette({ dark: a }: ZenAccent): ChatPalette {
  return {
    layout: 'telegram',
    showWallpaper: false,
    showTimestamps: false,
    showDateDividers: true,
    background: ZEN_BRAND_COLORS.environment,
    // A lit warm panel on the ink canvas, the dark counterpart of the ink bubble.
    sentBubble: '#35302A',
    receivedBubble: ZEN_DARK_NEUTRALS.surface,
    sentText: ZEN_DARK_NEUTRALS.textPrimary,
    receivedText: ZEN_DARK_NEUTRALS.textPrimary,
    sentTimestamp: ZEN_DARK_NEUTRALS.textSecondary,
    receivedTimestamp: ZEN_DARK_NEUTRALS.textTertiary,
    outboundSentClock: ZEN_DARK_NEUTRALS.textSecondary,
    composerBackground: ZEN_DARK_NEUTRALS.surface,
    composerBorder: ZEN_DARK_NEUTRALS.border,
    composerDock: TRANSPARENT,
    link: a.accentStrong,
    patternIcon: TRANSPARENT,
  };
}

export const ZEN_DARK_CHAT_PALETTE: ChatPalette = buildDarkChatPalette(DEFAULT_ACCENT);

export function buildSurfacePalette(colors: AppColors): SurfacePalette {
  return {
    card: colors.bgSurface,
    cardStrong: colors.bgElevated,
    subtle: colors.surfaceSubtle,
    border: colors.border,
    sectionLabel: colors.textTertiary,
  };
}

export const ZEN_LIGHT_SURFACE_PALETTE: SurfacePalette = buildSurfacePalette(ZEN_LIGHT_APP_COLORS);

export const ZEN_DARK_SURFACE_PALETTE: SurfacePalette = buildSurfacePalette(ZEN_DARK_APP_COLORS);

export function buildLightMaterials({ ramp }: ZenAccent): MaterialPalette {
  return {
    // The landing's sticky bar: paper at 82% over a blur.
    chrome: 'rgba(251,250,247,0.86)',
    regular: 'rgba(255,255,255,0.92)',
    thick: 'rgba(255,255,255,0.97)',
    thin: 'rgba(255,255,255,0.68)',
    stroke: 'rgba(22,20,18,0.08)',
    separator: 'rgba(22,20,18,0.10)',
    tint: rgba(ramp[600], 0.12),
  };
}

export const ZEN_LIGHT_MATERIALS: MaterialPalette = buildLightMaterials(DEFAULT_ACCENT);

export function buildDarkMaterials({ dark: a }: ZenAccent): MaterialPalette {
  return {
    chrome: 'rgba(20,18,16,0.86)',
    regular: 'rgba(38,34,30,0.92)',
    thick: 'rgba(44,40,35,0.97)',
    thin: 'rgba(52,47,42,0.64)',
    // The landing's dark hairline: white at 10%.
    stroke: 'rgba(255,255,255,0.08)',
    separator: 'rgba(255,255,255,0.10)',
    tint: rgba(a.accent, 0.18),
  };
}

export const ZEN_DARK_MATERIALS: MaterialPalette = buildDarkMaterials(DEFAULT_ACCENT);

export function buildLightDataVisualization({ ramp }: ZenAccent): DataVisualizationPalette {
  return { activityRamp: [ramp[100], ramp[300], ramp[500], ramp[700]] };
}

export const ZEN_LIGHT_DATA_VISUALIZATION: DataVisualizationPalette =
  buildLightDataVisualization(DEFAULT_ACCENT);

export function buildDarkDataVisualization({ ramp }: ZenAccent): DataVisualizationPalette {
  return { activityRamp: [ramp[900], ramp[700], ramp[500], ramp[300]] };
}

export const ZEN_DARK_DATA_VISUALIZATION: DataVisualizationPalette =
  buildDarkDataVisualization(DEFAULT_ACCENT);
