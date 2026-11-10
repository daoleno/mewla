import { colorScheme } from "nsh";

/**
 * The colours the pages draw with outside classes (canvas strokes, status marks),
 * from app/DESIGN.md: paper and ink, the six Work states, vermilion for the seal only.
 */
const light = {
  ink: "#161412",
  soft: "#57514A",
  faint: "#857E73",
  line: "#DDD6C9",
  hairline: "#ECE7DE",
  paper: "#FBFAF7",
  card: "#FFFFFF",
  tint: "#F5F3EE",
  ready: "#2F6B4F",
  running: "#2C55C0",
  warning: "#9A6212",
  failed: "#7D1F35",
  blocked: "#736C61",
  seal: "#C8372B",
};
const dark: typeof light = {
  ink: "#F4F0EA",
  soft: "#C9C1B6",
  faint: "#8A8378",
  line: "#3F3933",
  hairline: "#2A2622",
  paper: "#141210",
  card: "#201D19",
  tint: "#2C2823",
  ready: "#74C79B",
  running: "#9AB6FF",
  warning: "#EDBA5A",
  failed: "#F2A0B1",
  blocked: "#9C9589",
  seal: "#D2412F",
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
