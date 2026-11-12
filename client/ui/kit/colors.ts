import { colorScheme } from "nsh";

/**
 * The colours the pages draw with outside classes (canvas strokes, status marks, icons): the
 * Soft palette of soft-final SPEC §10.1, the same values as ui/theme.json. Red (`seal`) means
 * waiting for you and nothing else; a state's tone names its role and reads in ink or soft.
 */
const light = {
  ink: "#131416",
  soft: "#62656D",
  faint: "#A6A9B0",
  hair: "#EDEDEE",
  track: "#E7E8E8",
  canvas: "#F2F3F5",
  surface: "#FFFFFF",
  seal: "#D63C2F",
  sealInk: "#C93327",
  /** On a seal fill (Approve's check): white in both schemes. */
  onSeal: "#FFFFFF",
  add: "#1F8A4C",
  del: "#C93327",
  ready: "#62656D",
  running: "#131416",
  warning: "#62656D",
  failed: "#131416",
  blocked: "#A6A9B0",
};
const dark: typeof light = {
  ink: "#F2F3F5",
  soft: "#A2A5AC",
  faint: "#62656C",
  hair: "#2E2F32",
  track: "#3C3D3F",
  canvas: "#0F1012",
  surface: "#1C1D20",
  seal: "#D23E31",
  sealInk: "#FF7A6B",
  onSeal: "#FFFFFF",
  add: "#5FCB88",
  del: "#FF7A6B",
  ready: "#A2A5AC",
  running: "#F2F3F5",
  warning: "#A2A5AC",
  failed: "#F2F3F5",
  blocked: "#62656C",
};

export type ColorName = keyof typeof light;

/** The colour now, following the system's appearance (read it inside JSX or a canvas `draw`). */
export function color(name: ColorName): string {
  return (colorScheme() === "dark" ? dark : light)[name];
}

/** `#rrggbb` at `alpha` (0..1), for washes under a chart line. */
export function withAlpha(hex: string, alpha: number): string {
  const a = Math.round(Math.max(0, Math.min(1, alpha)) * 255).toString(16).padStart(2, "0");
  return `${hex}${a}`;
}
