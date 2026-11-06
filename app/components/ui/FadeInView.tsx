import React, { useEffect } from "react";
import type { StyleProp, ViewStyle } from "react-native";
import Animated, {
  Easing,
  ReduceMotion,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withTiming,
} from "react-native-reanimated";
import { Timing } from "../../constants/motion";

interface FadeInViewProps {
  /** Fade in when this mounts; false (or reduced motion) simply shows it. */
  animate: boolean;
  style?: StyleProp<ViewStyle>;
  children: React.ReactNode;
}

/**
 * A short ease-out fade for a block that just appeared after a person's own
 * action or because something new arrived. It starts transparent on its very
 * first paint, so there is no full-strength frame before the fade, and it
 * never animates content that is only scrolling into view.
 */
export function FadeInView({ animate, style, children }: FadeInViewProps) {
  const reducedMotion = useReducedMotion();
  const fade = animate && !reducedMotion;
  const opacity = useSharedValue(fade ? 0 : 1);
  useEffect(() => {
    if (!fade) return;
    opacity.value = withTiming(1, {
      duration: Timing.quick,
      easing: Easing.out(Easing.quad),
      reduceMotion: ReduceMotion.System,
    });
  }, [fade, opacity]);
  const animatedStyle = useAnimatedStyle(() => ({ opacity: opacity.value }));
  return <Animated.View style={[style, animatedStyle]}>{children}</Animated.View>;
}
