import React from "react";
import { Platform, type StyleProp, type ViewStyle } from "react-native";
import Svg, { Path } from "react-native-svg";
import { MEWLA_GLYPHS, type MewlaGlyph } from "./mewlaGlyphs";

export type IconName = keyof typeof MEWLA_GLYPHS;

export interface IconProps {
  name: IconName;
  color: string;
  size?: number;
  style?: StyleProp<ViewStyle>;
}

const STROKE = 1.5;

// Hidden from assistive tech. react-native-svg hands web props straight to
// the DOM <svg>, where the native-only names are unknown, so web uses ARIA.
const DECORATIVE_PROPS: object =
  Platform.OS === "web"
    ? { "aria-hidden": true }
    : {
        accessibilityElementsHidden: true,
        importantForAccessibility: "no-hide-descendants",
      };

/**
 * The app's one icon vocabulary: Mewla's own drawings on a 24 grid, stroked
 * at 1.5 with round caps and joins (mewlaGlyphs.ts). Decorative; the control
 * around it carries the accessibility label.
 */
export function Icon({ name, color, size = 24, style }: IconProps) {
  const glyph: MewlaGlyph = MEWLA_GLYPHS[name];
  return (
    <Svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      style={style}
      pointerEvents="none"
      {...DECORATIVE_PROPS}
    >
      {glyph.strokes.map((d, index) => (
        <Path
          key={`s${index}`}
          d={d}
          fill="none"
          stroke={color}
          strokeWidth={STROKE}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      ))}
      {glyph.fills?.map((d, index) => <Path key={`f${index}`} d={d} fill={color} fillRule="evenodd" />)}
    </Svg>
  );
}
