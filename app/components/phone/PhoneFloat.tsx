import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Keyboard, PixelRatio, Platform, StyleSheet, View, useWindowDimensions, type LayoutChangeEvent } from "react-native";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Animated, {
  Easing,
  cancelAnimation,
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withSequence,
  withTiming,
} from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { usePathname, useRouter } from "expo-router";
import { useAppTheme } from "../../constants/tokens";
import { MewlaPhone, PhoneLiveView } from "../../modules/mewla-phone/src";
import { getPhoneFloatCorner, getPhoneFloatSize, setPhoneFloatCorner, setPhoneFloatSize } from "../../services/storage";
import { usePhoneUse } from "../../store/phoneUse";
import { IconButton } from "../ui/IconButton";
import {
  DEFAULT_FLOAT_CORNER,
  FLOAT_DEFAULT,
  clampFloatFraction,
  floatOrigin,
  floatSize,
  nearestFloatCorner,
  parseFloatCorner,
  parseFloatFraction,
  phoneAgentWorking,
  phoneFloatVisible,
  phoneMode,
  resizedFloatWidth,
  toggledFloatFraction,
  type FloatCorner,
} from "./phoneSetup";

const EDGE = 12;
/** Under the page header. */
const HEADER = 56;
/** The chat composer, with room for a few lines of draft: the float stays above it. */
const COMPOSER = 140;
/** Frames the small window asks for; the Phone page and the floating window ask for more. */
const FLOAT_FPS = 4;
/** Frame width asked for: the window's own pixels, in steps so a resize does not ask again at every move. */
const FRAME_STEP = 120;
const FRAME_MIN = 240;
const FRAME_MAX = 720;
const CONTROLS_MS = 4_000;
const BUTTON = 34;
const RADIUS = 10;
/** The resize handle: a touch area around a small grip in the float's inner corner. */
const HANDLE = 40;
const GRIP = 16;

function useKeyboardHeight() {
  const [height, setHeight] = useState(0);
  useEffect(() => {
    const show = Keyboard.addListener(Platform.OS === "ios" ? "keyboardWillShow" : "keyboardDidShow", (event) =>
      setHeight(event.endCoordinates.height),
    );
    const hide = Keyboard.addListener(Platform.OS === "ios" ? "keyboardWillHide" : "keyboardDidHide", () => setHeight(0));
    return () => {
      show.remove();
      hide.remove();
    };
  }, []);
  return height;
}

/**
 * The agent's screen as a small live window over every page while an agent uses the phone, so the
 * person keeps chatting and watches it at once. It is mounted once above navigation; the Phone
 * page is the full view and hides it. Leaving the App floats the screen natively (see PipOverlay).
 */
export function PhoneFloat() {
  const { status } = usePhoneUse();
  const pathname = usePathname();

  // Leaving the App from any page floats the agent's screen, not only from the Phone page.
  const arm = phoneAgentWorking(status);
  const w = status?.w ?? 0;
  const h = status?.h ?? 0;
  useEffect(() => {
    if (!arm || !MewlaPhone?.setPipAuto) return;
    MewlaPhone.setPipAuto(true, w, h);
    return () => MewlaPhone?.setPipAuto?.(false, w, h);
  }, [arm, w, h]);

  if (!PhoneLiveView || !status || !phoneFloatVisible(status, pathname)) return null;
  return <Float />;
}

type Gesturing = "none" | "drag" | "resize";

function Float() {
  const { colors } = useAppTheme();
  const phone = usePhoneUse();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const window = useWindowDimensions();
  const keyboard = useKeyboardHeight();
  const status = phone.status;
  const mode = status ? phoneMode(status) : "idle";
  const handoff = mode === "handoff";
  const driving = mode === "driving";

  const [box, setBox] = useState({ width: window.width, height: window.height });
  const [corner, setCorner] = useState<FloatCorner>(DEFAULT_FLOAT_CORNER);
  const [fraction, setFraction] = useState(FLOAT_DEFAULT);
  const [controls, setControls] = useState(false);
  const placed = useRef(false);
  const gesturing = useRef<Gesturing>("none");

  useEffect(() => {
    let alive = true;
    void Promise.all([getPhoneFloatCorner(), getPhoneFloatSize()])
      .then(([savedCorner, savedSize]) => {
        if (!alive) return;
        setCorner(parseFloatCorner(savedCorner));
        setFraction(parseFloatFraction(savedSize));
      })
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);

  const area = useMemo(
    () => ({
      left: 0,
      top: insets.top + HEADER,
      right: box.width,
      bottom: box.height - (keyboard > 0 ? keyboard : insets.bottom) - COMPOSER,
    }),
    [box.height, box.width, insets.bottom, insets.top, keyboard],
  );
  const ratio = status && status.w > 0 && status.h > 0 ? status.w / status.h : 9 / 20;
  // The window is the agent display's exact shape, so the picture fills it; it never reaches the composer or the keyboard.
  const size = useMemo(
    () => floatSize(ratio, fraction, box, Math.max(0, area.bottom - area.top - 2 * EDGE)),
    [area.bottom, area.top, box, fraction, ratio],
  );

  const x = useSharedValue(area.right - EDGE - size.width);
  const y = useSharedValue(area.top + EDGE);
  const origin = useMemo(() => floatOrigin(corner, area, size, EDGE), [area, corner, size]);

  // The gestures are made once and read what changes through this, so a resize that re-renders at
  // every move never replaces the gesture that is in progress.
  const latest = useRef({ area, size, corner, ratio, fraction, box });
  latest.current = { area, size, corner, ratio, fraction, box };

  // The float follows its corner: the keyboard, a rotation or a new size move it, a drag does not.
  useEffect(() => {
    if (gesturing.current === "drag") return;
    if (!placed.current || gesturing.current === "resize") {
      placed.current = true;
      x.value = origin.x;
      y.value = origin.y;
      return;
    }
    x.value = withTiming(origin.x, { duration: 220, easing: Easing.out(Easing.cubic) });
    y.value = withTiming(origin.y, { duration: 220, easing: Easing.out(Easing.cubic) });
  }, [origin.x, origin.y, x, y]);

  const remember = useCallback((next: number) => {
    void setPhoneFloatSize(next).catch(() => undefined);
  }, []);

  const start = useSharedValue({ x: 0, y: 0 });
  const startWidth = useRef(0);
  const startFraction = useRef(FLOAT_DEFAULT);
  const gestures = useMemo(() => {
    const pan = Gesture.Pan()
      .runOnJS(true)
      .maxPointers(1)
      .minDistance(6)
      .onStart(() => {
        gesturing.current = "drag";
        cancelAnimation(x);
        cancelAnimation(y);
        start.value = { x: x.value, y: y.value };
      })
      .onUpdate((event) => {
        x.value = start.value.x + event.translationX;
        y.value = start.value.y + event.translationY;
      })
      .onEnd((event) => {
        const l = latest.current;
        // A flick carries a little further than the finger.
        const centerX = start.value.x + event.translationX + event.velocityX * 0.08 + l.size.width / 2;
        const centerY = start.value.y + event.translationY + event.velocityY * 0.08 + l.size.height / 2;
        const next = nearestFloatCorner({ x: centerX, y: centerY }, l.area);
        gesturing.current = "none";
        setCorner(next);
        void setPhoneFloatCorner(next).catch(() => undefined);
        const target = floatOrigin(next, l.area, l.size, EDGE);
        x.value = withTiming(target.x, { duration: 220, easing: Easing.out(Easing.cubic) });
        y.value = withTiming(target.y, { duration: 220, easing: Easing.out(Easing.cubic) });
      })
      .onFinalize(() => {
        if (gesturing.current === "drag") gesturing.current = "none";
      });
    const tap = Gesture.Tap()
      .runOnJS(true)
      .maxDuration(400)
      .onEnd((_event, success) => {
        if (success) setControls((shown) => !shown);
      });
    // A double tap goes between the default and the large size.
    const doubleTap = Gesture.Tap()
      .runOnJS(true)
      .numberOfTaps(2)
      .maxDuration(400)
      .onEnd((_event, success) => {
        if (!success) return;
        const next = toggledFloatFraction(latest.current.fraction);
        setFraction(next);
        remember(next);
      });
    // Two fingers resize continuously, the shape locked.
    const pinch = Gesture.Pinch()
      .runOnJS(true)
      .onStart(() => {
        gesturing.current = "resize";
        startFraction.current = latest.current.fraction;
      })
      .onUpdate((event) => setFraction(clampFloatFraction(startFraction.current * event.scale)))
      .onEnd(() => remember(latest.current.fraction))
      .onFinalize(() => {
        if (gesturing.current === "resize") gesturing.current = "none";
      });
    return Gesture.Simultaneous(pinch, Gesture.Race(pan, Gesture.Exclusive(doubleTap, tap)));
  }, [remember, start, x, y]);

  // The corner handle resizes with one finger along the diagonal.
  const handle = useMemo(
    () =>
      Gesture.Pan()
        .runOnJS(true)
        .minDistance(0)
        .onStart(() => {
          gesturing.current = "resize";
          startWidth.current = latest.current.size.width;
        })
        .onUpdate((event) => {
          const l = latest.current;
          const width = resizedFloatWidth(startWidth.current, l.ratio, { x: event.translationX, y: event.translationY }, l.corner);
          setFraction(clampFloatFraction(width / l.box.width));
        })
        .onEnd(() => remember(latest.current.fraction))
        .onFinalize(() => {
          if (gesturing.current === "resize") gesturing.current = "none";
        }),
    [remember],
  );

  // The controls tuck themselves away; a handoff keeps asking.
  useEffect(() => {
    if (!controls) return;
    const timer = setTimeout(() => setControls(false), CONTROLS_MS);
    return () => clearTimeout(timer);
  }, [controls]);

  // A handoff asks for attention with a ring that keeps breathing in the seal colour.
  const pulse = useSharedValue(0);
  useEffect(() => {
    if (!handoff) {
      cancelAnimation(pulse);
      pulse.value = 0;
      return;
    }
    pulse.value = withRepeat(
      withSequence(withTiming(1, { duration: 700, easing: Easing.inOut(Easing.quad) }), withTiming(0.2, { duration: 700, easing: Easing.inOut(Easing.quad) })),
      -1,
    );
    return () => cancelAnimation(pulse);
  }, [handoff, pulse]);

  const frameStyle = useAnimatedStyle(() => ({ transform: [{ translateX: x.value }, { translateY: y.value }] }));
  const ringStyle = useAnimatedStyle(() => ({ opacity: pulse.value }));

  const onLayout = (event: LayoutChangeEvent) => {
    const { width, height } = event.nativeEvent.layout;
    setBox((previous) => (previous.width === width && previous.height === height ? previous : { width, height }));
  };

  const openPhone = () => {
    setControls(false);
    router.push("/phone");
  };
  // Taking over opens the full view, where the person's finger can drive the agent's screen.
  const takeOver = () => {
    phone.takeOver();
    openPhone();
  };

  const frameWidth = Math.min(FRAME_MAX, Math.max(FRAME_MIN, Math.ceil((size.width * PixelRatio.get()) / FRAME_STEP) * FRAME_STEP));
  // The handle sits in the corner opposite the one the float is anchored to, so dragging it outward grows the window.
  const handleLeft = corner.endsWith("right");
  const handleTop = corner.startsWith("bottom");

  return (
    <View style={StyleSheet.absoluteFill} pointerEvents="box-none" onLayout={onLayout}>
      <Animated.View
        accessibilityLabel="Agent screen"
        style={[
          styles.frame,
          { width: size.width, height: size.height, backgroundColor: "#000", shadowColor: "#000" },
          frameStyle,
        ]}
      >
        <GestureDetector gesture={gestures}>
          <View style={StyleSheet.absoluteFill}>
            <View style={StyleSheet.absoluteFill} pointerEvents="none">
              {PhoneLiveView ? <PhoneLiveView style={StyleSheet.absoluteFill} fps={FLOAT_FPS} maxWidth={frameWidth} /> : null}
            </View>
            {controls ? (
              <View style={styles.controls}>
                <IconButton icon="expand" size={BUTTON} iconSize={16} color="#fff" accessibilityLabel="Open" onPress={openPhone} style={styles.button} />
                {handoff ? (
                  <IconButton icon="check" size={BUTTON} iconSize={16} color="#fff" accessibilityLabel="Done" onPress={phone.handoffDone} style={styles.button} />
                ) : driving ? (
                  <IconButton icon="return-forward" size={BUTTON} iconSize={16} color="#fff" accessibilityLabel="Give back" onPress={phone.giveBack} style={styles.button} />
                ) : (
                  <IconButton icon="hand" size={BUTTON} iconSize={16} color="#fff" accessibilityLabel="Take over" onPress={takeOver} style={styles.button} />
                )}
                <IconButton icon="stop" size={BUTTON} iconSize={16} color="#fff" accessibilityLabel="Stop" onPress={phone.stop} style={styles.button} />
              </View>
            ) : null}
          </View>
        </GestureDetector>
        <View pointerEvents="none" style={[styles.edge, { borderColor: colors.borderSubtle }]} />
        {handoff ? <Animated.View pointerEvents="none" style={[styles.ring, { borderColor: colors.seal }, ringStyle]} /> : null}
        <GestureDetector gesture={handle}>
          <View
            accessibilityLabel="Resize"
            style={[styles.handle, handleLeft ? styles.handleLeft : styles.handleRight, handleTop ? styles.handleTop : styles.handleBottom]}
          >
            <View
              style={[
                styles.grip,
                handleLeft ? { borderLeftWidth: 3, borderTopLeftRadius: handleTop ? 6 : 0, borderBottomLeftRadius: handleTop ? 0 : 6 } : { borderRightWidth: 3, borderTopRightRadius: handleTop ? 6 : 0, borderBottomRightRadius: handleTop ? 0 : 6 },
                handleTop ? { borderTopWidth: 3 } : { borderBottomWidth: 3 },
              ]}
            />
          </View>
        </GestureDetector>
      </Animated.View>
    </View>
  );
}

const fill = { position: "absolute", top: 0, left: 0, right: 0, bottom: 0 } as const;

const styles = StyleSheet.create({
  frame: {
    position: "absolute",
    left: 0,
    top: 0,
    borderRadius: RADIUS,
    overflow: "hidden",
    elevation: 8,
    shadowOpacity: 0.3,
    shadowRadius: 8,
    shadowOffset: { width: 0, height: 3 },
  },
  // The hairline is drawn over the picture, so it takes none of the window's size.
  edge: { ...fill, borderWidth: StyleSheet.hairlineWidth, borderRadius: RADIUS },
  ring: { ...fill, borderWidth: 3, borderRadius: RADIUS },
  controls: {
    ...fill,
    backgroundColor: "rgba(0,0,0,0.55)",
    alignItems: "center",
    justifyContent: "center",
    gap: 10,
  },
  button: { backgroundColor: "rgba(255,255,255,0.18)" },
  handle: { position: "absolute", width: HANDLE, height: HANDLE, padding: 8 },
  handleLeft: { left: 0, alignItems: "flex-start" },
  handleRight: { right: 0, alignItems: "flex-end" },
  handleTop: { top: 0, justifyContent: "flex-start" },
  handleBottom: { bottom: 0, justifyContent: "flex-end" },
  grip: { width: GRIP, height: GRIP, borderColor: "rgba(255,255,255,0.9)" },
});
