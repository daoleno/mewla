import React from "react";
import Svg, { Line, Rect } from "react-native-svg";
import { useAppTheme } from "../../constants/tokens";

const WIDTH = 104;
const HEIGHT = 88;

/**
 * Screen's empty state: a quiet monitor, dark and waiting, drawn in the same
 * soft ink and paper tint as the other empty states' seals. No colour, no glow.
 */
export function ScreenArt() {
  const { colors, theme } = useAppTheme();
  const ink = colors.textTertiary;
  return (
    <Svg width={WIDTH} height={HEIGHT} viewBox={`0 0 ${WIDTH} ${HEIGHT}`}>
      <Rect x={8} y={8} width={88} height={58} rx={11} fill={theme.materials.tint} stroke={ink} strokeWidth={2} />
      {/* What it will show: a window's title bar and two lines, faint. */}
      <Rect x={22} y={22} width={22} height={4} rx={2} fill={ink} opacity={0.5} />
      <Rect x={22} y={34} width={60} height={4} rx={2} fill={ink} opacity={0.25} />
      <Rect x={22} y={44} width={42} height={4} rx={2} fill={ink} opacity={0.25} />
      <Line x1={52} y1={66} x2={52} y2={76} stroke={ink} strokeWidth={2} strokeLinecap="round" />
      <Line x1={36} y1={78} x2={68} y2={78} stroke={ink} strokeWidth={2} strokeLinecap="round" />
    </Svg>
  );
}
