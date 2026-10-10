import React, { useCallback, useMemo, useRef, useState } from "react";
import { Pressable, TextInput, type StyleProp, type ViewStyle } from "react-native";
import * as Haptics from "expo-haptics";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Animated, {
  cancelAnimation,
  Easing,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withRepeat,
  withSequence,
  withSpring,
  withTiming,
} from "react-native-reanimated";
import type { BrainCatState } from "./brainCatState";
import { catCanPlay, isDoubleTap, PET_HOLD_MS, tossOffset, tossTilt } from "./catPlay";
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
  cheer,
  stillPortrait,
}: {
  state: BrainCatState;
  size: number;
  animate?: boolean;
  hopStyle: ReturnType<typeof useCatHop>["style"];
  cheer?: number;
  stillPortrait?: boolean;
}) {
  return (
    <Animated.View style={[{ transformOrigin: "50% 100%" }, hopStyle]}>
      <PetSprite state={state} size={size} animate={animate} cheer={cheer} stillPortrait={stillPortrait} />
    </Animated.View>
  );
}

/** The cat keeps still while the user types (any text field has focus). */
function typingNow(): boolean {
  const fields = TextInput.State as {
    currentlyFocusedInput?: () => unknown;
    currentlyFocusedField?: () => unknown;
  };
  return (fields.currentlyFocusedInput?.() ?? fields.currentlyFocusedField?.()) != null;
}

/**
 * Play the user starts: a second tap is a happy hop, a hold pets the cat (it
 * purrs, a light haptic), and a hold then a drag picks it up for a small toss
 * that springs back home. Nothing moves on its own, nothing moves while the
 * user types, and reduced motion keeps only the haptics.
 */
function useCatPlay(state: BrainCatState, size: number, playable: boolean) {
  const reduced = useReducedMotion();
  const [cheer, setCheer] = useState(0);
  const purr = useSharedValue(0);
  const x = useSharedValue(0);
  const y = useSharedValue(0);
  const tilt = useSharedValue(0);
  const held = useSharedValue(0);
  const lastTapAt = useRef<number | null>(null);
  // A hold answers instead of the tap, so releasing it never presses.
  const answered = useRef(false);
  // While carried, the Pressable's own release (native cancels it when the
  // carry starts) must not end the purr.
  const carrying = useRef(false);
  const inSeal = petInSeal(state);
  const reach = size * (inSeal ? 0.25 : 0.5);

  const pressIn = useCallback(() => {
    answered.current = false;
  }, []);

  /** True when this tap was the second of a double-tap and has been answered. */
  const doubleTap = useCallback(() => {
    const now = Date.now();
    const second = isDoubleTap(lastTapAt.current, now);
    lastTapAt.current = second ? null : now;
    if (!second || !playable || typingNow()) return false;
    void Haptics.selectionAsync();
    if (reduced) return true;
    if (inSeal) {
      // The seal can't jump; the cat stirs twice instead.
      purr.value = withSequence(
        withTiming(1, { duration: 110 }),
        withTiming(0, { duration: 140 }),
        withTiming(1, { duration: 110 }),
        withTiming(0, { duration: 200 }),
      );
    } else {
      setCheer((count) => count + 1);
    }
    return true;
  }, [inSeal, playable, purr, reduced]);

  const startPet = useCallback(() => {
    if (answered.current || !playable || typingNow()) return;
    answered.current = true;
    void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    if (reduced) return;
    purr.value = withRepeat(
      withSequence(
        withTiming(1, { duration: 320, easing: Easing.inOut(Easing.quad) }),
        withTiming(0.2, { duration: 320, easing: Easing.inOut(Easing.quad) }),
      ),
      -1,
    );
  }, [playable, purr, reduced]);

  const endPet = useCallback(() => {
    cancelAnimation(purr);
    purr.value = withTiming(0, { duration: 180 });
  }, [purr]);

  const releasePress = useCallback(() => {
    if (!carrying.current) endPet();
  }, [endPet]);

  const settle = useCallback(() => {
    held.value = withTiming(0, { duration: 160 });
    x.value = withSpring(0, { damping: 9, stiffness: 180 });
    y.value = withSpring(0, { damping: 9, stiffness: 180 });
    tilt.value = withSpring(0, { damping: 10, stiffness: 160 });
  }, [held, tilt, x, y]);

  // The pan starts after the hold, so a scroll or a drawer swipe that moves
  // first is never caught; once it starts, the list and drawer wait.
  const carry = useMemo(
    () =>
      Gesture.Pan()
        .runOnJS(true)
        .enabled(playable && !reduced)
        .activateAfterLongPress(PET_HOLD_MS)
        .onStart(() => {
          startPet();
          if (!answered.current) return;
          carrying.current = true;
          held.value = withTiming(1, { duration: 120 });
        })
        .onUpdate((event) => {
          if (!carrying.current) return;
          const moved = Math.hypot(event.translationX, event.translationY);
          if (moved > 6) endPet();
          x.value = tossOffset(event.translationX, reach);
          y.value = tossOffset(event.translationY, reach);
          tilt.value = tossTilt(event.velocityX);
        })
        .onFinalize(() => {
          carrying.current = false;
          endPet();
          settle();
        }),
    [endPet, held, playable, reach, reduced, settle, startPet, tilt, x, y],
  );

  const style = useAnimatedStyle(() => {
    const p = purr.value;
    return {
      // Squash and lift pivot on the feet, like the hop.
      transformOrigin: "50% 100%",
      zIndex: held.value > 0 ? 10 : 0,
      transform: [
        { translateX: x.value },
        { translateY: y.value - size * 0.06 * held.value },
        { rotate: `${tilt.value}deg` },
        { scaleX: 1 + 0.035 * p },
        { scaleY: 1 - 0.045 * p + 0.03 * held.value },
      ],
    };
  });

  return {
    cheer,
    style,
    carry,
    pressIn,
    doubleTap,
    startPet,
    releasePress,
    /** A hold or a carry already answered this press. */
    consumed: () => answered.current,
  };
}

/**
 * The cat you can tap on its own. It answers for Brain (the caller decides
 * the answer) and its body says it heard you. Users can also play with it:
 * see useCatPlay.
 */
export function TappableCat({
  state,
  size,
  animate,
  style,
  onPress,
  accessibilityLabel,
  accessibilityHint,
  stillPortrait,
}: {
  state: BrainCatState;
  size: number;
  animate?: boolean;
  style?: StyleProp<ViewStyle>;
  onPress?: () => void;
  accessibilityLabel: string;
  accessibilityHint?: string;
  /** See PetSprite. */
  stillPortrait?: boolean;
}) {
  const hop = useCatHop(state, size);
  const play = useCatPlay(state, size, Boolean(onPress) && catCanPlay(state));
  const { consumed, doubleTap, pressIn, releasePress, startPet } = play;
  const press = useCallback(() => {
    if (consumed() || doubleTap()) return;
    hop.trigger();
    onPress?.();
  }, [consumed, doubleTap, hop, onPress]);
  const cat = (
    <HoppingCat state={state} size={size} animate={animate} hopStyle={hop.style} cheer={play.cheer} stillPortrait={stillPortrait} />
  );
  if (!onPress) {
    return <Animated.View style={style}>{cat}</Animated.View>;
  }
  return (
    // On touch web, a vertical swipe that starts on the cat still scrolls.
    <GestureDetector gesture={play.carry} touchAction="pan-y">
      <Animated.View style={[style, play.style]}>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={accessibilityLabel}
          accessibilityHint={accessibilityHint}
          onPressIn={pressIn}
          onPress={press}
          onLongPress={startPet}
          onPressOut={releasePress}
          delayLongPress={PET_HOLD_MS}
          hitSlop={8}
        >
          {cat}
        </Pressable>
      </Animated.View>
    </GestureDetector>
  );
}
