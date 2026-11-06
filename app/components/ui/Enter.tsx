import React from "react";
import { type StyleProp, type ViewStyle } from "react-native";
import Animated, {
  Easing,
  FadeIn,
  FadeInDown,
  ReduceMotion,
  ZoomIn,
} from "react-native-reanimated";
import { Timing } from "../../constants/motion";

export type EnterPreset = "fade" | "rise" | "pop";

interface FadeInProps {
  preset?: EnterPreset;
  delay?: number;
  style?: StyleProp<ViewStyle>;
  children: React.ReactNode;
}

/**
 * A wrapper that mounts its children with a gentle entrance — the shared
 * "something just appeared" motion used for empty states, list sections, and
 * first-paint content: a short ease-out, no spring, and none under reduced
 * motion.
 */
export function Enter({ preset = "rise", delay = 0, style, children }: FadeInProps) {
  const ease = Easing.out(Easing.cubic);
  switch (preset) {
    case "fade":
      return (
        <Animated.View
          entering={FadeIn.delay(delay).duration(Timing.base).easing(ease).reduceMotion(ReduceMotion.System)}
          style={style}
        >
          {children}
        </Animated.View>
      );
    case "pop":
      return (
        <Animated.View
          entering={ZoomIn.delay(delay).duration(Timing.base).easing(ease).reduceMotion(ReduceMotion.System)}
          style={style}
        >
          {children}
        </Animated.View>
      );
    case "rise":
    default:
      return (
        <Animated.View
          entering={FadeInDown.delay(delay).duration(Timing.base).easing(ease).reduceMotion(ReduceMotion.System)}
          style={style}
        >
          {children}
        </Animated.View>
      );
  }
}
