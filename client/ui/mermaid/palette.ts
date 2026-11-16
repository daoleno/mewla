import type { MermaidMode, MermaidPalette } from "../../core/mermaid/mermaid";
import theme from "../theme.json";

const tokens = theme.cssVars;

/** The app's colours as a diagram is shown in (core/mermaid `mermaidCssVariables`): nodes on the code block's card, groups a step apart. */
export const MERMAID_PALETTES: Record<MermaidMode, MermaidPalette> = {
  light: { surface: tokens.light.card, ink: tokens.light.foreground, soft: tokens.light["muted-foreground"], node: tokens.light.accent, border: tokens.light.faint, cluster: "#F7F7F8" },
  dark: { surface: tokens.dark.card, ink: tokens.dark.foreground, soft: tokens.dark["muted-foreground"], node: tokens.dark.accent, border: tokens.dark.faint, cluster: "#242528" },
};
