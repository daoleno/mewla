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

export type AccentRamp = { readonly [Stop in keyof typeof ZEN_SAGE]: string };

// The other accents reuse the sage lightness profile at their own hue, so
// every ramp stop lands on the same contrast against the warm-ink neutrals.
const ZEN_INK: AccentRamp = {
  50: '#F2F7FF',
  100: '#E2ECFE',
  200: '#C8DBFE',
  300: '#ACC8FB',
  400: '#90B1ED',
  500: '#6A8CC9',
  600: '#4E6BA2',
  700: '#3B527D',
  800: '#2C3D5C',
  900: '#202B3F',
  950: '#101621',
};

// Dusty clay (hue ~36) at 3/4 of sage chroma: warm, but held well apart from
// the saturated danger red and warning amber.
const ZEN_CLAY: AccentRamp = {
  50: '#FCF4F2',
  100: '#FBE7E1',
  200: '#F4D2C8',
  300: '#EABBAD',
  400: '#D9A192',
  500: '#B47C6C',
  600: '#905D50',
  700: '#6E473C',
  800: '#51352D',
  900: '#382621',
  950: '#1D1310',
};

// Near-monochrome warm stone: a trace of the canvas hue so it isn't dead grey.
const ZEN_STONE: AccentRamp = {
  50: '#F8F6F4',
  100: '#EFEBE5',
  200: '#E0D9CF',
  300: '#CFC6B7',
  400: '#BAAF9D',
  500: '#968A78',
  600: '#756B5A',
  700: '#595145',
  800: '#423C33',
  900: '#2E2B25',
  950: '#181512',
};

/**
 * Scheme accents sit between ramp stops: light needs 4.5:1 for white label
 * text on it, dark needs chroma without glowing on the ink canvas. `status`
 * paints running/in-progress marks; it is the accent unless the accent is too
 * neutral to stay apart from the grey "unknown" status.
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

export type ZenAccentId = 'sage' | 'ink' | 'clay' | 'stone';

export const ZEN_ACCENTS: readonly ZenAccent[] = [
  {
    id: 'sage',
    name: 'Sage',
    ramp: ZEN_SAGE,
    light: { accent: '#2E6F4A', accentStrong: '#235A3B', active: ZEN_SAGE[100], status: '#2E6F4A' },
    dark: { accent: '#8AD0A4', accentStrong: '#B3E6C4', active: '#1C3125', status: '#8AD0A4' },
  },
  {
    id: 'ink',
    name: 'Ink',
    ramp: ZEN_INK,
    light: { accent: '#435F93', accentStrong: '#354D78', active: ZEN_INK[100], status: '#435F93' },
    dark: { accent: '#9CBEFB', accentStrong: '#C3D8FF', active: '#222C3D', status: '#9CBEFB' },
  },
  {
    id: 'clay',
    name: 'Clay',
    ramp: ZEN_CLAY,
    light: { accent: '#825245', accentStrong: '#694237', active: ZEN_CLAY[100], status: '#825245' },
    dark: { accent: '#E7AE9E', accentStrong: '#F8CCC0', active: '#372723', status: '#E7AE9E' },
  },
  {
    id: 'stone',
    name: 'Stone',
    ramp: ZEN_STONE,
    // Stone sits next to the tertiary grey, so running work borrows ink.
    light: { accent: '#685F4F', accentStrong: '#544C3F', active: ZEN_STONE[100], status: '#435F93' },
    dark: { accent: '#C7BCA9', accentStrong: '#DFD6C8', active: '#2E2B26', status: '#9CBEFB' },
  },
];

export const DEFAULT_ACCENT_ID: ZenAccentId = 'sage';

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
  modalBackdrop: 'rgba(20,23,18,0.32)',
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
    logoDetail: ramp[900],
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

export function buildLightChatPalette({ ramp, light: a }: ZenAccent): ChatPalette {
  return {
    layout: 'telegram',
    showWallpaper: false,
    showTimestamps: false,
    showDateDividers: true,
    background: ZEN_LIGHT_NEUTRALS.canvas,
    sentBubble: ramp[200],
    receivedBubble: ZEN_LIGHT_NEUTRALS.surface,
    sentText: ZEN_LIGHT_NEUTRALS.textPrimary,
    receivedText: ZEN_LIGHT_NEUTRALS.textPrimary,
    sentTimestamp: ZEN_LIGHT_NEUTRALS.textSecondary,
    receivedTimestamp: ZEN_LIGHT_NEUTRALS.textTertiary,
    // Outside the bubble on chat.background — high-contrast accent, not outline chrome.
    outboundSentClock: ramp[700],
    composerBackground: ZEN_LIGHT_NEUTRALS.surface,
    composerBorder: ZEN_LIGHT_NEUTRALS.border,
    composerDock: TRANSPARENT,
    link: a.accentStrong,
    patternIcon: TRANSPARENT,
  };
}

export const ZEN_LIGHT_CHAT_PALETTE: ChatPalette = buildLightChatPalette(DEFAULT_ACCENT);

export function buildDarkChatPalette({ ramp, dark: a }: ZenAccent): ChatPalette {
  return {
    layout: 'telegram',
    showWallpaper: false,
    showTimestamps: false,
    showDateDividers: true,
    background: ZEN_BRAND_COLORS.environment,
    // Deep saturated accent on warm ink reads as a lit panel, not a murky mass.
    sentBubble: ramp[700],
    receivedBubble: ZEN_DARK_NEUTRALS.surface,
    sentText: ZEN_DARK_NEUTRALS.textPrimary,
    receivedText: ZEN_DARK_NEUTRALS.textPrimary,
    sentTimestamp: ramp[200],
    receivedTimestamp: ZEN_DARK_NEUTRALS.textTertiary,
    // Outside the bubble on the ink canvas — bright accent for status readability.
    outboundSentClock: ramp[200],
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
    chrome: 'rgba(245,243,238,0.88)',
    regular: 'rgba(254,253,251,0.92)',
    thick: 'rgba(254,253,251,0.97)',
    thin: 'rgba(254,253,251,0.68)',
    stroke: 'rgba(20,23,18,0.08)',
    separator: 'rgba(20,23,18,0.11)',
    tint: rgba(ramp[600], 0.12),
  };
}

export const ZEN_LIGHT_MATERIALS: MaterialPalette = buildLightMaterials(DEFAULT_ACCENT);

export function buildDarkMaterials({ dark: a }: ZenAccent): MaterialPalette {
  return {
    chrome: 'rgba(18,18,14,0.86)',
    regular: 'rgba(34,35,30,0.92)',
    thick: 'rgba(41,43,37,0.97)',
    thin: 'rgba(50,52,45,0.64)',
    stroke: 'rgba(255,250,235,0.08)',
    separator: 'rgba(255,250,235,0.10)',
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
