import { colorScheme, View } from "nsh";

/**
 * The tints laid over the Mac's materials (RESULT-DESIGN-PAIRS #2): the material blurs what is behind, the tint gives
 * it the design's colour. Bare, the perch header and the window's left pane read as grey bars.
 */
const MATERIAL_TINT = {
  /** The perch header (`popover`). */
  popover: { light: "rgba(246,247,249,0.84)", dark: "rgba(28,29,32,0.82)" },
  /** The window's left pane and Settings' sidebar (`sidebar`). */
  sidebar: { light: "rgba(242,243,245,0.72)", dark: "rgba(6,7,8,0.72)" },
} as const;

/** The tint over a material: the view's first child, its full size, taking no presses. */
export function MaterialTint(props: { material: keyof typeof MATERIAL_TINT }) {
  return (
    <View testID={`tint-${props.material}`} class="absolute inset-0"
      style={{ pointerEvents: "none", backgroundColor: colorScheme() === "dark" ? MATERIAL_TINT[props.material].dark : MATERIAL_TINT[props.material].light }} />
  );
}
