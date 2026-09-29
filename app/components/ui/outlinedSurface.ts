import type { ViewStyle } from "react-native";
import { ContinuousCorners } from "../../constants/tokens";

/**
 * Stroke for rounded outlined controls. A device hairline (1 physical px)
 * anti-aliases across two pixels on a small arc and reads as a gap at each
 * corner; one layout point keeps the ring continuous. Straight list dividers
 * keep StyleSheet.hairlineWidth.
 */
export const OutlineWidth = 1;

/**
 * The one outlined-control shape: continuous corners plus a full-point
 * stroke. Spread into styles of hand-built outlined buttons, chips and cards.
 */
export function outlinedSurface(
  radius: number,
  borderColor?: string,
): Pick<ViewStyle, "borderRadius" | "borderCurve" | "borderWidth" | "borderColor"> {
  return {
    borderRadius: radius,
    ...ContinuousCorners,
    borderWidth: OutlineWidth,
    ...(borderColor ? { borderColor } : null),
  };
}
