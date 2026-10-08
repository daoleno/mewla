import React from "react";
import type { StyleProp, ViewStyle } from "react-native";
import Svg, { Path } from "react-native-svg";
import { MEWLA_GLYPHS, type MewlaGlyph } from "./mewlaGlyphs";
import { PHOSPHOR_GLYPHS } from "./phosphorGlyphs";

export type IconName = keyof typeof PHOSPHOR_GLYPHS | keyof typeof MEWLA_GLYPHS;

export interface IconProps {
  name: IconName;
  color: string;
  size?: number;
  style?: StyleProp<ViewStyle>;
}

const MEWLA_STROKE = 24;

function isMewlaGlyph(name: IconName): name is keyof typeof MEWLA_GLYPHS {
  return name in MEWLA_GLYPHS;
}

/**
 * The app's one icon vocabulary: Phosphor's bold geometry, vendored as path
 * data, plus the hand-drawn Mewla glyphs. Decorative; the control around it
 * carries the accessibility label.
 */
export function Icon({ name, color, size = 24, style }: IconProps) {
  let body: React.ReactNode;
  if (isMewlaGlyph(name)) {
    const glyph: MewlaGlyph = MEWLA_GLYPHS[name];
    body = (
      <>
        {glyph.strokes.map((d) => (
          <Path
            key={d}
            d={d}
            fill="none"
            stroke={color}
            strokeWidth={MEWLA_STROKE}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        ))}
        {glyph.fills?.map((d) => <Path key={d} d={d} fill={color} />)}
      </>
    );
  } else {
    body = PHOSPHOR_GLYPHS[name].map((d) => <Path key={d} d={d} fill={color} />);
  }
  return (
    <Svg
      width={size}
      height={size}
      viewBox="0 0 256 256"
      style={style}
      pointerEvents="none"
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
    >
      {body}
    </Svg>
  );
}
