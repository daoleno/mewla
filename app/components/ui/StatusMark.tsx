import React, { useEffect } from "react";
import { View, type StyleProp, type ViewStyle } from "react-native";
import Animated, {
  cancelAnimation,
  Easing,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withRepeat,
  withTiming,
} from "react-native-reanimated";
import Svg, { Circle, Path, Rect } from "react-native-svg";
import { useAppTheme } from "../../constants/tokens";
import { WORK_STATUS_GLYPHS, workStatusInk, type WorkStatus } from "./workStatus";

interface StatusMarkProps {
  status: WorkStatus;
  size?: number;
  /** Overrides the state's ink, e.g. chat chrome colours. */
  color?: string;
  /** Fill behind the Ready check; defaults to the card surface. */
  knockout?: string;
  /** Running spins unless false or reduced motion is on. */
  animate?: boolean;
  style?: StyleProp<ViewStyle>;
}

/**
 * The 13 px Seal & Slip state glyph: Ready filled check, Running arc, Needs
 * you seal dot, Warning open triangle, Failed crossed box, Blocked dashed ring.
 * Decorative: the status word next to it carries the meaning for readers.
 */
export function StatusMark({ status, size = 13, color, knockout, animate = true, style }: StatusMarkProps) {
  const { colors } = useAppTheme();
  const ink = color ?? workStatusInk(status, {
    statusReady: colors.statusDone,
    statusRunning: colors.statusRunning,
    seal: colors.seal,
    sealText: colors.sealText,
    statusWarning: colors.statusWarning,
    statusFailed: colors.statusFailed,
    statusBlocked: colors.statusBlocked,
  });
  const glyph = WORK_STATUS_GLYPHS[status];
  const reducedMotion = useReducedMotion();
  const spin = glyph === "arc" && animate && !reducedMotion;
  const turn = useSharedValue(0);
  useEffect(() => {
    if (!spin) {
      cancelAnimation(turn);
      turn.value = 0;
      return;
    }
    turn.value = withRepeat(withTiming(1, { duration: 1400, easing: Easing.linear }), -1, false);
    return () => cancelAnimation(turn);
  }, [spin, turn]);
  const spinStyle = useAnimatedStyle(() => ({ transform: [{ rotate: `${turn.value * 360}deg` }] }));

  const line = { stroke: ink, strokeLinecap: "round" as const, strokeLinejoin: "round" as const, fill: "none" };
  let shape: React.ReactNode;
  switch (glyph) {
    case "check-disc":
      shape = (
        <>
          <Circle cx={8} cy={8} r={7} fill={ink} />
          <Path d="M4.8 8.2l2.2 2.1 4.2-4.4" {...line} stroke={knockout ?? colors.bgSurface} strokeWidth={1.7} />
        </>
      );
      break;
    case "arc":
      shape = (
        <>
          <Circle cx={8} cy={8} r={6} stroke={ink} strokeWidth={1.6} fill="none" opacity={0.25} />
          <Path d="M8 2a6 6 0 0 1 6 6" {...line} strokeWidth={1.8} />
        </>
      );
      break;
    case "seal-dot":
      shape = <Circle cx={8} cy={8} r={4} fill={ink} />;
      break;
    case "triangle":
      shape = (
        <>
          <Path d="M8 2.2L14.6 13.6H1.4Z" {...line} strokeWidth={1.5} />
          <Path d="M8 6.6v3" {...line} strokeWidth={1.6} />
        </>
      );
      break;
    case "crossed-box":
      shape = (
        <>
          <Rect x={1.8} y={1.8} width={12.4} height={12.4} rx={2} stroke={ink} strokeWidth={1.6} fill="none" />
          <Path d="M5.4 5.4l5.2 5.2M10.6 5.4l-5.2 5.2" {...line} strokeWidth={1.6} />
        </>
      );
      break;
    case "dashed-ring":
      shape = <Circle cx={8} cy={8} r={6} stroke={ink} strokeWidth={1.5} strokeDasharray="2.2 2" fill="none" />;
      break;
  }
  const svg = (
    <Svg width={size} height={size} viewBox="0 0 16 16">
      {shape}
    </Svg>
  );
  return (
    <View
      accessible={false}
      importantForAccessibility="no-hide-descendants"
      style={[{ width: size, height: size }, style]}
    >
      {spin ? <Animated.View style={spinStyle}>{svg}</Animated.View> : svg}
    </View>
  );
}
