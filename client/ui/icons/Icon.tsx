import { Image, token, type Tokens } from "nsh";
import { ICON_ASSETS } from "./assets";

export type IconName = keyof typeof ICON_ASSETS;
export type ColorToken = { [K in keyof Tokens]: Tokens[K] extends string ? K : never }[keyof Tokens];

/**
 * Mewla's one icon vocabulary (glyphs.ts: its own drawings on a 24 grid,
 * stroked at 1.5), as template images tinted with a theme color. Decorative:
 * the control around it carries the label.
 */
export function Icon(props: { name: IconName; color?: ColorToken; size?: number; class?: string }) {
  const size = props.size ?? 20;
  return <Image src={ICON_ASSETS[props.name]} class={props.class} style={{ width: size, height: size, tintColor: token(props.color ?? "muted-foreground") }} />;
}
