import React, { useEffect } from "react";
import Animated, {
  Easing,
  ReduceMotion,
  useAnimatedStyle,
  useSharedValue,
  withTiming,
} from "react-native-reanimated";
import { Timing } from "../../constants/motion";
import { Icon } from "../icons/Icon";

interface AnimatedChevronProps {
  expanded: boolean;
  size: number;
  color: string;
}

/**
 * One chevron that turns from pointing forward to pointing down, so opening a
 * disclosure reads as the same mark moving, not two marks swapping. A row that
 * mounts already open does not turn, and reduced motion jumps to the end.
 */
export function AnimatedChevron({ expanded, size, color }: AnimatedChevronProps) {
  const turn = useSharedValue(expanded ? 1 : 0);
  useEffect(() => {
    turn.value = withTiming(expanded ? 1 : 0, {
      duration: Timing.base,
      easing: Easing.out(Easing.cubic),
      reduceMotion: ReduceMotion.System,
    });
  }, [expanded, turn]);
  const style = useAnimatedStyle(() => ({
    transform: [{ rotate: `${turn.value * 90}deg` }],
  }));
  return (
    <Animated.View style={style}>
      <Icon name="chevron-right" size={size} color={color} />
    </Animated.View>
  );
}
