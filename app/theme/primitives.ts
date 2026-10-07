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
 * The chrome accent. Seal & Slip keeps the chrome neutral: controls, links,
 * selection and the primary button are ink on paper (paper on ink in dark).
 * `active` is the quiet selected-row fill.
 */
export interface AccentScheme {
  accent: string;
  accentStrong: string;
  active: string;
}

export interface ZenAccent {
  id: ZenAccentId;
  name: string;
  light: AccentScheme;
  dark: AccentScheme;
}

/**
 * Ink is the one chrome accent. Vermilion is not an accent: it is the seal
 * (the cat, the logo), Send and "Needs you", and lives in the `seal` tokens.
 * A stored legacy preference (vermilion, sage, ...) resolves to ink.
 */
export type ZenAccentId = 'ink';

export const ZEN_ACCENTS: readonly ZenAccent[] = [
  {
    id: 'ink',
    name: 'Ink',
    light: { accent: '#161412', accentStrong: '#161412', active: '#EFEBE3' },
    dark: { accent: '#F4F0EA', accentStrong: '#F4F0EA', active: '#322D28' },
  },
];

export const DEFAULT_ACCENT_ID: ZenAccentId = 'ink';

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

/**
 * The seal's three jobs: the cat and logo, Send, and "Needs you". `seal` is
 * the fill (white glyphs on it), `sealText` is vermilion set as words on the
 * paper, `sealSoft` the faint wash behind a perched cat.
 */
export const ZEN_LIGHT_SEAL = {
  seal: ZEN_VERMILION[600],
  sealText: '#BC3328',
  sealSoft: ZEN_VERMILION[100],
  onSeal: '#FFFFFF',
} as const;

export const ZEN_DARK_SEAL = {
  seal: '#D2412F',
  sealText: '#FF9466',
  sealSoft: '#3A1F1B',
  onSeal: '#FFFFFF',
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
  borderSubtle: '#ECE7DE',
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

/**
 * Seal & Slip status semantics. Every state has its own glyph (StatusMark), so
 * none relies on hue: Ready green check, Running blue arc, Warning amber
 * triangle, Failed oxblood crossed box, Blocked stone dashed ring. Failure is
 * never vermilion; "Needs you" is the seal.
 */
export const ZEN_LIGHT_STATUS = {
  ready: '#2F6B4F',
  running: '#2C55C0',
  warning: '#9A6212',
  failed: '#7D1F35',
  blocked: '#736C61',
  dangerSoft: '#F7E8EB',
  warningSoft: '#FBF0DD',
  successSoft: '#E6F1EA',
  runningSoft: '#E8EEFB',
} as const;

export const ZEN_DARK_STATUS = {
  ready: '#74C79B',
  running: '#9AB6FF',
  warning: '#EDBA5A',
  failed: '#F2A0B1',
  blocked: '#9C9589',
  dangerSoft: '#3A1F26',
  warningSoft: '#33270F',
  successSoft: '#15291E',
  runningSoft: '#1B2440',
} as const;

export const ZEN_LIGHT_OVERLAYS = {
  modalBackdrop: 'rgba(22,20,18,0.32)',
} as const;

export const ZEN_DARK_OVERLAYS = {
  modalBackdrop: 'rgba(0,0,0,0.6)',
} as const;

export function buildLightAppColors({ light: a }: ZenAccent): AppColors {
  const st = ZEN_LIGHT_STATUS;
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
    ...ZEN_LIGHT_SEAL,
    // The Zen mark keeps its sage-ink ribbon until the rename replaces it.
    logoDetail: ZEN_SAGE[900],
    statusFailed: st.failed,
    statusBlocked: st.blocked,
    statusWarning: st.warning,
    statusUnknown: ZEN_LIGHT_NEUTRALS.textTertiary,
    statusRunning: st.running,
    statusDone: st.ready,
    zenGreen: st.ready,
    priorityUrgent: st.failed,
    priorityHigh: st.warning,
    priorityMedium: st.running,
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
    textOnAccent: ZEN_LIGHT_NEUTRALS.canvas,
    focusRing: st.running,
    selectionBackground: rgba(st.running, 0.18),
    promptGreen: st.ready,
    promptYellow: st.warning,
    warning: st.warning,
    dangerText: st.failed,
    success: st.ready,
    disabledText: ZEN_LIGHT_NEUTRALS.textTertiary,
    dangerSoft: st.dangerSoft,
    warningSoft: st.warningSoft,
    successSoft: st.successSoft,
    runningSoft: st.runningSoft,
    shadowColor: ZEN_BRAND_COLORS.environment,
  };
}

export const ZEN_LIGHT_APP_COLORS: AppColors = buildLightAppColors(DEFAULT_ACCENT);

export function buildDarkAppColors({ dark: a }: ZenAccent): AppColors {
  const st = ZEN_DARK_STATUS;
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
    ...ZEN_DARK_SEAL,
    logoDetail: ZEN_BRAND_COLORS.ivory,
    statusFailed: st.failed,
    statusBlocked: st.blocked,
    statusWarning: st.warning,
    statusUnknown: ZEN_DARK_NEUTRALS.textTertiary,
    statusRunning: st.running,
    statusDone: st.ready,
    zenGreen: st.ready,
    priorityUrgent: st.failed,
    priorityHigh: st.warning,
    priorityMedium: st.running,
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
    focusRing: st.running,
    selectionBackground: rgba(st.running, 0.28),
    promptGreen: st.ready,
    promptYellow: st.warning,
    warning: st.warning,
    dangerText: st.failed,
    success: st.ready,
    disabledText: ZEN_DARK_NEUTRALS.textTertiary,
    dangerSoft: st.dangerSoft,
    warningSoft: st.warningSoft,
    successSoft: st.successSoft,
    runningSoft: st.runningSoft,
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
    // Seal & Slip: your words sit on a quiet paper tint, Brain's on the page.
    sentBubble: '#F0ECE4',
    receivedBubble: ZEN_LIGHT_NEUTRALS.surface,
    sentText: ZEN_LIGHT_NEUTRALS.textPrimary,
    receivedText: ZEN_LIGHT_NEUTRALS.textPrimary,
    sentTimestamp: ZEN_LIGHT_NEUTRALS.textTertiary,
    receivedTimestamp: ZEN_LIGHT_NEUTRALS.textTertiary,
    // Outside the bubble on chat.background: quiet ink, not outline chrome.
    outboundSentClock: ZEN_LIGHT_NEUTRALS.textSecondary,
    composerBackground: ZEN_LIGHT_NEUTRALS.surface,
    composerBorder: ZEN_LIGHT_NEUTRALS.borderSubtle,
    composerDock: TRANSPARENT,
    // Links are ink and underlined; colour is reserved for status.
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
    // A lit warm panel on the ink canvas, the dark counterpart of the tint.
    sentBubble: '#2E2A25',
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

export function buildLightMaterials(_accent: ZenAccent): MaterialPalette {
  return {
    // The landing's sticky bar: paper at 82% over a blur.
    chrome: 'rgba(251,250,247,0.86)',
    regular: 'rgba(255,255,255,0.92)',
    thick: 'rgba(255,255,255,0.97)',
    thin: 'rgba(255,255,255,0.68)',
    stroke: 'rgba(22,20,18,0.08)',
    separator: 'rgba(22,20,18,0.10)',
    // A paper tint, not a colour: selected and tinted controls stay neutral.
    tint: 'rgba(22,20,18,0.06)',
  };
}

export const ZEN_LIGHT_MATERIALS: MaterialPalette = buildLightMaterials(DEFAULT_ACCENT);

export function buildDarkMaterials(_accent: ZenAccent): MaterialPalette {
  return {
    chrome: 'rgba(20,18,16,0.86)',
    regular: 'rgba(38,34,30,0.92)',
    thick: 'rgba(44,40,35,0.97)',
    thin: 'rgba(52,47,42,0.64)',
    // The landing's dark hairline: white at 10%.
    stroke: 'rgba(255,255,255,0.08)',
    separator: 'rgba(255,255,255,0.10)',
    tint: 'rgba(255,255,255,0.08)',
  };
}

export const ZEN_DARK_MATERIALS: MaterialPalette = buildDarkMaterials(DEFAULT_ACCENT);

// Activity is good news, so the heatmap ramps toward the Ready green.
export function buildLightDataVisualization(_accent: ZenAccent): DataVisualizationPalette {
  return { activityRamp: ['#E3EFE7', '#A9CDB6', '#5E9A76', '#2F6B4F'] };
}

export const ZEN_LIGHT_DATA_VISUALIZATION: DataVisualizationPalette =
  buildLightDataVisualization(DEFAULT_ACCENT);

export function buildDarkDataVisualization(_accent: ZenAccent): DataVisualizationPalette {
  return { activityRamp: ['#1F3528', '#2E6045', '#4E9670', '#7FD0A5'] };
}

export const ZEN_DARK_DATA_VISUALIZATION: DataVisualizationPalette =
  buildDarkDataVisualization(DEFAULT_ACCENT);
