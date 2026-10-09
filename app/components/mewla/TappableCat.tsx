import React, { useCallback } from "react";
import { Pressable, type StyleProp, type ViewStyle } from "react-native";
import * as Haptics from "expo-haptics";
import Animated, {
  Easing,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withSequence,
  withTiming,
} from "react-native-reanimated";
import type { BrainCatState } from "./brainCatState";
import { PetSprite } from "../pets/PetSprite";
import { petInSeal } from "../pets/petModel";

/**
 * The cat's "I heard you": a standing cat hops, a cat in the seal stirs (a
 * small squash), with a selection haptic. Reduced motion keeps the haptic.
 */
export function useCatHop(state: BrainCatState, size: number) {
  const reduced = useReducedMotion();
  const hop = useSharedValue(0);
  const inSeal = petInSeal(state);
  const style = useAnimatedStyle(() => {
    const t = hop.value;
    return inSeal
      ? { transform: [{ scaleX: 1 + 0.04 * t }, { scaleY: 1 - 0.05 * t }] }
      : { transform: [{ translateY: -size * 0.12 * t }, { scaleY: 1 + 0.03 * t }] };
  });
  const trigger = useCallback(() => {
    void Haptics.selectionAsync();
    if (reduced) return;
    hop.value = withSequence(
      withTiming(1, { duration: 130, easing: Easing.out(Easing.quad) }),
      withTiming(0, { duration: 220, easing: Easing.bezier(0.2, 0.8, 0.2, 1) }),
    );
  }, [hop, reduced]);
  return { style, trigger };
}

/** The cat drawn on its hop layer (the transform pivots on its feet). */
export function HoppingCat({
  state,
  size,
  animate,
  hopStyle,
}: {
  state: BrainCatState;
  size: number;
  animate?: boolean;
  hopStyle: ReturnType<typeof useCatHop>["style"];
}) {
  return (
    <Animated.View style={[{ transformOrigin: "50% 100%" }, hopStyle]}>
      <PetSprite state={state} size={size} animate={animate} />
    </Animated.View>
  );
}

/**
 * The cat you can tap on its own. It answers for Brain (the caller decides
 * the answer) and its body says it heard you.
 */
export function TappableCat({
  state,
  size,
  animate,
  style,
  onPress,
  accessibilityLabel,
  accessibilityHint,
}: {
  state: BrainCatState;
  size: number;
  animate?: boolean;
  style?: StyleProp<ViewStyle>;
  onPress?: () => void;
  accessibilityLabel: string;
  accessibilityHint?: string;
}) {
  const hop = useCatHop(state, size);
  const press = useCallback(() => {
    hop.trigger();
    onPress?.();
  }, [hop, onPress]);
  const cat = <HoppingCat state={state} size={size} animate={animate} hopStyle={hop.style} />;
  if (!onPress) {
    return <Animated.View style={style}>{cat}</Animated.View>;
  }
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      accessibilityHint={accessibilityHint}
      onPress={press}
      hitSlop={8}
      style={style}
    >
      {cat}
    </Pressable>
  );
}
