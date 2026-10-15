import React, { useEffect, useRef, useState } from "react";
import { StyleSheet } from "react-native";
import Animated, {
  Easing,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withDelay,
  withTiming,
} from "react-native-reanimated";
import type { BrainCatState } from "../mewla/brainCatState";
import { sealBehind, type CatBox } from "../mewla/launchTiming";
import { petInSeal } from "../pets/petModel";
import { PetSprite } from "../pets/PetSprite";

/** Clips are drawn at this side and scaled, so a hop can change size smoothly. */
const BASE = 164;
const HOP_MS = 440;
const HOP_LIFT = 70;

/**
 * The one cat across every onboarding step: it hops between the welcome spot
 * and the corner, and back for the connected moment, where the seal stamps in
 * behind it. `seal` names the box the seal belongs behind, or null.
 */
export function OnboardingCat({ box, state, seal }: { box: CatBox; state: BrainCatState; seal: CatBox | null }) {
  const reducedMotion = useReducedMotion();
  const from = useSharedValue(box);
  const to = useSharedValue(box);
  const progress = useSharedValue(1);
  const sealIn = useSharedValue(seal ? 1 : 0);
  const [hops, setHops] = useState(0);
  const placed = useRef(box);

  useEffect(() => {
    const last = placed.current;
    if (last.cx === box.cx && last.cy === box.cy && last.size === box.size) return;
    placed.current = box;
    const p = progress.value;
    const a = from.value;
    const b = to.value;
    from.value = { cx: a.cx + (b.cx - a.cx) * p, cy: a.cy + (b.cy - a.cy) * p, size: a.size + (b.size - a.size) * p };
    to.value = box;
    progress.value = 0;
    progress.value = withTiming(1, { duration: reducedMotion ? 0 : HOP_MS, easing: Easing.inOut(Easing.quad) });
    setHops((count) => count + 1);
  }, [box, from, progress, reducedMotion, to]);

  const sealKey = seal ? `${seal.cx}:${seal.cy}` : null;
  useEffect(() => {
    // The seal stamps in once the cat has landed, and lifts as it leaves.
    sealIn.value = sealKey
      ? withDelay(reducedMotion ? 0 : HOP_MS - 60, withTiming(1, { duration: reducedMotion ? 0 : 260, easing: Easing.out(Easing.back(2)) }))
      : withTiming(0, { duration: reducedMotion ? 0 : 200 });
  }, [reducedMotion, sealIn, sealKey]);

  const catStyle = useAnimatedStyle(() => {
    const p = progress.value;
    const a = from.value;
    const b = to.value;
    const cx = a.cx + (b.cx - a.cx) * p;
    const cy = a.cy + (b.cy - a.cy) * p - HOP_LIFT * 4 * p * (1 - p);
    const size = a.size + (b.size - a.size) * p;
    return { transform: [{ translateX: cx - BASE / 2 }, { translateY: cy - BASE / 2 }, { scale: size / BASE }] };
  });
  // The seal stays drawn while it lifts away.
  const lastSeal = useRef(seal);
  if (seal) lastSeal.current = seal;
  const behind = lastSeal.current ? sealBehind(lastSeal.current) : null;
  const sealStyle = useAnimatedStyle(() => ({
    opacity: Math.min(1, sealIn.value * 2),
    transform: [{ scale: 1.5 - 0.5 * sealIn.value }],
  }));

  return (
    <>
      {behind && !petInSeal(state) ? (
        <Animated.View
          style={[styles.box, { left: behind.cx - behind.size / 2, top: behind.cy - behind.size / 2, width: behind.size, height: behind.size }, sealStyle]}
        >
          <PetSprite state="homeless" size={behind.size} />
        </Animated.View>
      ) : null}
      <Animated.View style={[styles.box, styles.cat, catStyle]}>
        <PetSprite state={state} size={BASE} cheer={hops} />
      </Animated.View>
    </>
  );
}

const styles = StyleSheet.create({
  box: { position: "absolute", pointerEvents: "none" },
  cat: { left: 0, top: 0, width: BASE, height: BASE },
});
