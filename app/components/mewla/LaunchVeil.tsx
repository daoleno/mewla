import React, { useEffect, useRef, useState } from "react";
import { StyleSheet, View, type LayoutChangeEvent } from "react-native";
import { Image } from "expo-image";
import * as SplashScreen from "expo-splash-screen";
import Animated, {
  Easing,
  runOnJS,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withDelay,
  withTiming,
} from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppTheme } from "../../constants/tokens";
import { useThemeContext } from "../../theme";
import { heroCatBox } from "../onboarding/onboardingModel";
import { findPetPack } from "../pets/petModel";
import { DEFAULT_PET_ID, PET_PACKS } from "../pets/petPacks";
import {
  LAUNCH,
  SPLASH_IMAGE_SIDE,
  launchExitAt,
  sealBehind,
  splashSealBox,
  type CatBox,
  type LaunchDestination,
} from "./launchTiming";

const SPLASH_IMAGE = require("../../assets/branding/mewla-splash.png");
/** A waking clip that never shows (a slow decode) falls back to the still splash. */
const WAKE_GIVE_UP_MS = 700;
/** The native splash goes even if this screen never draws. */
const SPLASH_SAFETY_MS = 2500;

/**
 * Cold start only (see launchTiming.ts): drawn over the app from its first
 * frame, it takes over from the native splash pixel for pixel, plays the cat
 * waking out of the seal, and leaves as soon as the app is ready. On the way
 * to pairing the cat flies to where the welcome screen's cat stands.
 */
export function LaunchVeil({ destination, onDone }: { destination: LaunchDestination | null; onDone(): void }) {
  const { colors } = useAppTheme();
  const reducedMotion = useReducedMotion();
  const pack = findPetPack(PET_PACKS, useThemeContext().petId, DEFAULT_PET_ID);
  const insets = useSafeAreaInsets();
  const [frame, setFrame] = useState<{ width: number; height: number } | null>(null);
  const [splashShown, setSplashShown] = useState(false);
  const [wokeAt, setWokeAt] = useState<number | null>(null);
  const [wakeStalled, setWakeStalled] = useState(false);
  const [readyAt, setReadyAt] = useState<number | null>(null);
  const [awake, setAwake] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const still = reducedMotion || wakeStalled;

  const veil = useSharedValue(1);
  const face = useSharedValue(1);
  const cat = useSharedValue(0);
  const flight = useSharedValue(0);
  const leaveRef = useRef(false);

  useEffect(() => {
    const timer = setTimeout(() => SplashScreen.hide(), SPLASH_SAFETY_MS);
    return () => clearTimeout(timer);
  }, []);

  // The splash image here is the native one in the same place; swap them.
  useEffect(() => {
    if (frame && splashShown) SplashScreen.hide();
  }, [frame, splashShown]);

  useEffect(() => {
    if (destination && readyAt === null) setReadyAt(Date.now());
  }, [destination, readyAt]);

  useEffect(() => {
    if (!splashShown || still || wokeAt !== null) return;
    const timer = setTimeout(() => setWakeStalled(true), WAKE_GIVE_UP_MS);
    return () => clearTimeout(timer);
  }, [splashShown, still, wokeAt]);

  useEffect(() => {
    if (wokeAt === null || still) return;
    face.value = withTiming(0, { duration: LAUNCH.crossfadeMs, easing: Easing.linear });
    cat.value = withTiming(1, { duration: LAUNCH.crossfadeMs, easing: Easing.linear });
    // A slow start keeps the cat looking around in front of its seal; one
    // already on its way out keeps the clip's last frame.
    const timer = setTimeout(() => {
      if (!leaveRef.current) setAwake(true);
    }, pack.clips.waking.durationMs);
    return () => clearTimeout(timer);
  }, [cat, face, pack, still, wokeAt]);

  const startedAt = still ? (splashShown ? 0 : null) : wokeAt;
  useEffect(() => {
    if (startedAt === null || readyAt === null || leaveRef.current) return;
    const origin = still ? Date.now() : startedAt;
    const exitAt = launchExitAt({ readyAt: still ? 0 : readyAt - origin, reducedMotion: still });
    if (exitAt === null) return;
    const timer = setTimeout(() => {
      leaveRef.current = true;
      setLeaving(true);
    }, Math.max(0, origin + exitAt - Date.now()));
    return () => clearTimeout(timer);
  }, [readyAt, startedAt, still]);

  useEffect(() => {
    if (!leaving) return;
    const finish = (finished?: boolean) => {
      "worklet";
      if (finished) runOnJS(onDone)();
    };
    if (still) {
      veil.value = withTiming(0, { duration: LAUNCH.reducedExitMs }, finish);
      return;
    }
    const exit = LAUNCH.exitMs;
    flight.value = withTiming(1, { duration: exit, easing: Easing.inOut(Easing.quad) });
    // The background goes in the second half, so the cat lands before the
    // welcome screen's own cat shows through it.
    veil.value = withDelay(exit * 0.45, withTiming(0, { duration: exit * 0.55 }, finish));
  }, [flight, leaving, onDone, still, veil]);

  const from = frame ? splashSealBox(frame.width, frame.height) : null;
  const to: CatBox | null = !frame || !from
    ? null
    : destination === "onboarding"
      ? heroCatBox({ width: frame.width, height: frame.height, top: insets.top, bottom: insets.bottom })
      : { ...from, cy: from.cy - 28 };
  const veilStyle = useAnimatedStyle(() => ({ opacity: veil.value }));
  const faceStyle = useAnimatedStyle(() => ({ opacity: face.value }));
  const catStyle = useAnimatedStyle(() => {
    if (!from || !to) return { opacity: cat.value };
    const p = flight.value;
    return {
      opacity: cat.value,
      transform: [
        { translateX: (to.cx - from.cx) * p },
        { translateY: (to.cy - from.cy) * p },
        { scale: 1 + (to.size / from.size - 1) * p },
      ],
    };
  });

  const onLayout = (event: LayoutChangeEvent) => {
    const { width, height } = event.nativeEvent.layout;
    setFrame((current) => (current?.width === width && current.height === height ? current : { width, height }));
  };
  const waking = pack.clips.waking;
  const box = from;
  const bleed = box ? (waking.bleed ?? 0) * box.size : 0;
  const seal = box ? sealBehind(box) : null;
  return (
    <Animated.View
      style={[StyleSheet.absoluteFill, styles.veil, { backgroundColor: colors.bgPrimary, pointerEvents: leaving ? "none" : "auto" }, veilStyle]}
      onLayout={onLayout}
      accessible
      accessibilityRole="progressbar"
      accessibilityLabel="Starting Mewla"
    >
      {frame && box ? (
        <>
          {!still && splashShown ? (
            <Animated.View style={[styles.box, { left: box.cx - box.size / 2, top: box.cy - box.size / 2, width: box.size, height: box.size }, catStyle]}>
              {awake ? (
                <>
                  {seal ? <Image source={pack.clips.homeless.source} contentFit="contain"
                    style={[styles.box, { left: seal.cx - box.cx, top: seal.cy - box.cy, width: box.size, height: box.size }]} /> : null}
                  <Image source={pack.clips.delegating.source} contentFit="contain" style={{ width: box.size, height: box.size }} />
                </>
              ) : (
                <Image
                  source={waking.source}
                  contentFit="contain"
                  onDisplay={() => setWokeAt((current) => current ?? Date.now())}
                  style={[styles.box, { left: -bleed, top: -bleed, width: box.size + 2 * bleed, height: box.size + 2 * bleed }]}
                />
              )}
            </Animated.View>
          ) : null}
          <Animated.View
            style={[
              styles.box,
              {
                left: frame.width / 2 - SPLASH_IMAGE_SIDE / 2,
                top: frame.height / 2 - SPLASH_IMAGE_SIDE / 2,
                width: SPLASH_IMAGE_SIDE,
                height: SPLASH_IMAGE_SIDE,
              },
              faceStyle,
            ]}
          >
            <Image
              source={SPLASH_IMAGE}
              contentFit="contain"
              onDisplay={() => setSplashShown(true)}
              style={{ width: SPLASH_IMAGE_SIDE, height: SPLASH_IMAGE_SIDE }}
            />
          </Animated.View>
        </>
      ) : null}
    </Animated.View>
  );
}

const styles = StyleSheet.create({
  veil: { zIndex: 1000, elevation: 1000 },
  box: { position: "absolute" },
});
