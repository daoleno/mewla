import {
  ZEN_BRAND_COLORS,
  ZEN_DARK_NEUTRALS,
  ZEN_DARK_STATUS,
  ZEN_LIGHT_NEUTRALS,
  ZEN_LIGHT_STATUS,
} from '../theme/primitives';

export interface TerminalThemePalette {
  background: string;
  foreground: string;
  cursor: string;
  cursorAccent: string;
  selectionBackground: string;
  selectionInactiveBackground?: string;
  black: string;
  red: string;
  green: string;
  yellow: string;
  blue: string;
  magenta: string;
  cyan: string;
  white: string;
  brightBlack: string;
  brightRed: string;
  brightGreen: string;
  brightYellow: string;
  brightBlue: string;
  brightMagenta: string;
  brightCyan: string;
  brightWhite: string;
}

export interface TerminalThemeChrome {
  appBackground: string;
  surface: string;
  surfaceMuted: string;
  surfaceActive: string;
  /** Rounded composer input field — slightly lifted from appBackground. */
  composerInput: string;
  border: string;
  borderStrong: string;
  text: string;
  textMuted: string;
  textSubtle: string;
  textOnAccent: string;
  accent: string;
  accentSoft: string;
  disabledSurface: string;
  focus: string;
  link: string;
  danger: string;
  dangerSoft: string;
  /** The seal: Send, "Needs you" and the cat. Never a status of failure. */
  seal: string;
  sealText: string;
  onSeal: string;
  /** Seal & Slip Work states (see StatusMark). */
  statusReady: string;
  statusRunning: string;
  statusWarning: string;
  statusBlocked: string;
  overlay: string;
  shadowColor: string;
}

const ANSI_COLOR_KEYS = [
  'black',
  'red',
  'green',
  'yellow',
  'blue',
  'magenta',
  'cyan',
  'white',
  'brightBlack',
  'brightRed',
  'brightGreen',
  'brightYellow',
  'brightBlue',
  'brightMagenta',
  'brightCyan',
  'brightWhite',
] as const;

const XTERM_CUBE_LEVELS = [0, 95, 135, 175, 215, 255] as const;

export type TerminalThemeName =
  | 'light'
  | 'dark';

export type TerminalSystemColorScheme =
  | 'light'
  | 'dark'
  | 'unspecified'
  | null
  | undefined;

/**
 * The terminal sits on the app's paper (light) and the landing's warm ink
 * (dark), with an ink cursor and the Running blue as selection. The 16 ANSI
 * colours are warm-tuned for TUIs: every normal colour reads at 4.5:1 on its
 * canvas, every bright one at 3:1, and red is a crimson that stays clear of
 * the seal vermilion so a deleted line never reads as "Needs you"
 * (terminalThemes.test.ts). Light "white" is a stone grey, as in most light
 * terminal themes, so programs that print white stay legible on paper.
 */
export const TerminalThemes: Record<TerminalThemeName, TerminalThemePalette> = {
  dark: {
    background: ZEN_BRAND_COLORS.environment,
    // The landing's terminal ink: a step softer than text for a dense grid.
    foreground: '#E9E4DB',
    cursor: ZEN_DARK_NEUTRALS.textPrimary,
    cursorAccent: ZEN_BRAND_COLORS.environment,
    selectionBackground: withAlpha(ZEN_DARK_STATUS.running, 0.28),
    selectionInactiveBackground: withAlpha(ZEN_DARK_STATUS.running, 0.14),
    black: ZEN_DARK_NEUTRALS.elevated,
    red: '#F08592',
    green: ZEN_DARK_STATUS.ready,
    yellow: ZEN_DARK_STATUS.warning,
    blue: ZEN_DARK_STATUS.running,
    magenta: '#D49BE6',
    cyan: '#6FC6C4',
    white: '#D6D0C5',
    brightBlack: ZEN_DARK_NEUTRALS.borderStrong,
    brightRed: '#F7A8B2',
    brightGreen: '#9ADDB7',
    brightYellow: '#F5D08A',
    brightBlue: '#BCCEFF',
    brightMagenta: '#E4BDF0',
    brightCyan: '#97DCDA',
    brightWhite: ZEN_DARK_NEUTRALS.textPrimary,
  },
  light: {
    background: ZEN_LIGHT_NEUTRALS.canvas,
    foreground: ZEN_LIGHT_NEUTRALS.textPrimary,
    cursor: ZEN_LIGHT_NEUTRALS.textPrimary,
    cursorAccent: ZEN_LIGHT_NEUTRALS.canvas,
    selectionBackground: withAlpha(ZEN_LIGHT_STATUS.running, 0.18),
    selectionInactiveBackground: withAlpha(ZEN_LIGHT_STATUS.running, 0.1),
    black: '#2A2622',
    red: '#A01F3A',
    green: '#2E7550',
    yellow: '#8F5D10',
    blue: ZEN_LIGHT_STATUS.running,
    magenta: '#8A3D9E',
    cyan: '#1E6B73',
    white: '#6F685E',
    brightBlack: '#7A7368',
    brightRed: '#C23A6A',
    brightGreen: '#3B8A60',
    brightYellow: '#A86F18',
    brightBlue: '#4A6FD6',
    brightMagenta: '#A052B4',
    brightCyan: '#2A8189',
    brightWhite: '#8F887C',
  },
};

export const DefaultTerminalThemeName: TerminalThemeName = 'dark';

export function isTerminalThemeName(value: string): value is TerminalThemeName {
  return Object.prototype.hasOwnProperty.call(TerminalThemes, value);
}

export function resolveTerminalThemeName(
  colorScheme: TerminalSystemColorScheme = 'dark',
): TerminalThemeName {
  return colorScheme === 'light' ? 'light' : 'dark';
}

export function resolveTerminalTheme(
  name: TerminalThemeName = DefaultTerminalThemeName,
): TerminalThemePalette {
  return TerminalThemes[name];
}

export function buildTerminalPalette(theme: TerminalThemePalette): string[] {
  const palette = ANSI_COLOR_KEYS.map((key) => theme[key]);

  for (const red of XTERM_CUBE_LEVELS) {
    for (const green of XTERM_CUBE_LEVELS) {
      for (const blue of XTERM_CUBE_LEVELS) {
        palette.push(rgbToHex(red, green, blue));
      }
    }
  }

  for (let index = 0; index < 24; index += 1) {
    const level = 8 + index * 10;
    palette.push(rgbToHex(level, level, level));
  }

  return palette;
}

export function buildTerminalChrome(theme: TerminalThemePalette): TerminalThemeChrome {
  const surface = mixHex(theme.background, theme.foreground, 0.06);
  const surfaceMuted = mixHex(theme.background, theme.foreground, 0.035);

  return {
    appBackground: surface,
    surface,
    surfaceMuted,
    surfaceActive: mixHex(theme.background, theme.cursor, 0.14),
    composerInput: mixHex(theme.background, theme.foreground, 0.08),
    border: withAlpha(theme.foreground, 0.08),
    borderStrong: withAlpha(theme.cursor, 0.22),
    text: theme.foreground,
    textMuted: mixHex(theme.foreground, theme.background, 0.38),
    textSubtle: mixHex(theme.foreground, theme.background, 0.60),
    textOnAccent: theme.cursorAccent,
    accent: theme.cursor,
    accentSoft: withAlpha(theme.cursor, 0.14),
    disabledSurface: surfaceMuted,
    focus: theme.cursor,
    link: theme.blue,
    danger: theme.red,
    dangerSoft: withAlpha(theme.red, 0.14),
    // Terminal-only chrome has no seal; ANSI stands in for Send and status.
    seal: theme.red,
    sealText: theme.red,
    onSeal: theme.background,
    statusReady: theme.green,
    statusRunning: theme.blue,
    statusWarning: theme.yellow,
    statusBlocked: mixHex(theme.foreground, theme.background, 0.45),
    overlay: withAlpha(theme.background, 0.94),
    shadowColor: isLightTerminalTheme(theme) ? theme.foreground : '#000000',
  };
}

export function isLightTerminalTheme(theme: TerminalThemePalette): boolean {
  const { red, green, blue } = parseHex(theme.background);
  const luminance = (0.299 * red + 0.587 * green + 0.114 * blue) / 255;
  return luminance > 0.62;
}

function rgbToHex(red: number, green: number, blue: number): string {
  return (
    '#' +
    red.toString(16).padStart(2, '0') +
    green.toString(16).padStart(2, '0') +
    blue.toString(16).padStart(2, '0')
  );
}

function withAlpha(hex: string, alpha: number): string {
  const { red, green, blue } = parseHex(hex);
  return `rgba(${red}, ${green}, ${blue}, ${clamp(alpha, 0, 1)})`;
}

function mixHex(from: string, to: string, weight: number): string {
  const start = parseHex(from);
  const end = parseHex(to);
  const factor = clamp(weight, 0, 1);
  return rgbToHex(
    Math.round(start.red + (end.red - start.red) * factor),
    Math.round(start.green + (end.green - start.green) * factor),
    Math.round(start.blue + (end.blue - start.blue) * factor),
  );
}

function parseHex(value: string): { red: number; green: number; blue: number } {
  const normalized = value.trim().replace('#', '');
  if (!/^[0-9a-fA-F]{6}$/.test(normalized)) {
    throw new Error(`Expected 6-digit hex color, received "${value}"`);
  }

  return {
    red: parseInt(normalized.slice(0, 2), 16),
    green: parseInt(normalized.slice(2, 4), 16),
    blue: parseInt(normalized.slice(4, 6), 16),
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}
