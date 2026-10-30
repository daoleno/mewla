import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Keyboard, Platform, StyleSheet, View, useWindowDimensions, type LayoutChangeEvent } from "react-native";
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
import { getPhoneFloatCorner, setPhoneFloatCorner } from "../../services/storage";
import { usePhoneUse } from "../../store/phoneUse";
import { IconButton } from "../ui/IconButton";
import {
  DEFAULT_FLOAT_CORNER,
  floatOrigin,
  floatSize,
  floatWidth,
  nearestFloatCorner,
  parseFloatCorner,
  phoneAgentWorking,
  phoneFloatVisible,
  phoneMode,
  type FloatCorner,
} from "./phoneSetup";

const EDGE = 12;
/** Under the page header. */
const HEADER = 56;
/** The chat composer, with room for a few lines of draft: the float stays above it. */
const COMPOSER = 140;
/** Frames the small window asks for; the Phone page and the floating window ask for more. */
const FLOAT_FPS = 4;
/** Frame width asked for: enough pixels for the small window, the full frame for the large one. */
const SMALL_FRAME_WIDTH = 480;
const LARGE_FRAME_WIDTH = 720;
const CONTROLS_MS = 4_000;
const BUTTON = 34;

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
  const [controls, setControls] = useState(false);
  const [large, setLarge] = useState(false);
  const placed = useRef(false);

  useEffect(() => {
    let alive = true;
    void getPhoneFloatCorner()
      .then((value) => {
        if (alive) setCorner(parseFloatCorner(value));
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
  const size = useMemo(
    () => floatSize(ratio, floatWidth(box.width, large), Math.max(240, (area.bottom - area.top) - 2 * EDGE)),
    [area.bottom, area.top, box.width, large, ratio],
  );

  const x = useSharedValue(area.right - EDGE - size.width);
  const y = useSharedValue(area.top + EDGE);
  const dragging = useRef(false);
  const origin = useMemo(() => floatOrigin(corner, area, size, EDGE), [area, corner, size]);

  // The float follows its corner: the keyboard, a rotation or a new size move it, a drag does not.
  useEffect(() => {
    if (dragging.current) return;
    if (!placed.current) {
      placed.current = true;
      x.value = origin.x;
      y.value = origin.y;
      return;
    }
    x.value = withTiming(origin.x, { duration: 220, easing: Easing.out(Easing.cubic) });
    y.value = withTiming(origin.y, { duration: 220, easing: Easing.out(Easing.cubic) });
  }, [origin.x, origin.y, x, y]);

  const drop = useCallback(
    (nextCorner: FloatCorner) => {
      dragging.current = false;
      setCorner(nextCorner);
      void setPhoneFloatCorner(nextCorner).catch(() => undefined);
      const target = floatOrigin(nextCorner, area, size, EDGE);
      x.value = withTiming(target.x, { duration: 220, easing: Easing.out(Easing.cubic) });
      y.value = withTiming(target.y, { duration: 220, easing: Easing.out(Easing.cubic) });
    },
    [area, size, x, y],
  );

  const start = useSharedValue({ x: 0, y: 0 });
  const gesture = useMemo(() => {
    const pan = Gesture.Pan()
      .runOnJS(true)
      .minDistance(6)
      .onStart(() => {
        dragging.current = true;
        cancelAnimation(x);
        cancelAnimation(y);
        start.value = { x: x.value, y: y.value };
      })
      .onUpdate((event) => {
        x.value = start.value.x + event.translationX;
        y.value = start.value.y + event.translationY;
      })
      .onEnd((event) => {
        // A flick carries a little further than the finger.
        const centerX = start.value.x + event.translationX + event.velocityX * 0.08 + size.width / 2;
        const centerY = start.value.y + event.translationY + event.velocityY * 0.08 + size.height / 2;
        drop(nearestFloatCorner({ x: centerX, y: centerY }, area));
      })
      .onFinalize((_event, success) => {
        if (!success) dragging.current = false;
      });
    const tap = Gesture.Tap()
      .runOnJS(true)
      .maxDuration(400)
      .onEnd((_event, success) => {
        if (success) setControls((shown) => !shown);
      });
    // A pinch out or a double tap enlarges the window; a pinch in or another double tap shrinks it.
    const pinch = Gesture.Pinch()
      .runOnJS(true)
      .onEnd((event) => {
        if (event.scale > 1.15) setLarge(true);
        else if (event.scale < 0.87) setLarge(false);
      });
    const doubleTap = Gesture.Tap()
      .runOnJS(true)
      .numberOfTaps(2)
      .maxDuration(400)
      .onEnd((_event, success) => {
        if (success) setLarge((value) => !value);
      });
    return Gesture.Race(pan, pinch, Gesture.Exclusive(doubleTap, tap));
  }, [area, drop, size.height, size.width, start, x, y]);

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

  return (
    <View style={StyleSheet.absoluteFill} pointerEvents="box-none" onLayout={onLayout}>
      <GestureDetector gesture={gesture}>
        <Animated.View
          accessibilityLabel="Agent screen"
          style={[
            styles.frame,
            { width: size.width, height: size.height, backgroundColor: "#000", borderColor: colors.borderSubtle, shadowColor: "#000" },
            frameStyle,
          ]}
        >
          <View style={StyleSheet.absoluteFill} pointerEvents="none">
            {PhoneLiveView ? <PhoneLiveView
                style={StyleSheet.absoluteFill}
                fps={FLOAT_FPS}
                maxWidth={large ? LARGE_FRAME_WIDTH : SMALL_FRAME_WIDTH}
              /> : null}
          </View>
          {handoff ? (
            <Animated.View pointerEvents="none" style={[styles.ring, { borderColor: colors.seal }, ringStyle]} />
          ) : null}
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
        </Animated.View>
      </GestureDetector>
    </View>
  );
}

const styles = StyleSheet.create({
  frame: {
    position: "absolute",
    left: 0,
    top: 0,
    borderRadius: 14,
    overflow: "hidden",
    borderWidth: StyleSheet.hairlineWidth,
    elevation: 8,
    shadowOpacity: 0.3,
    shadowRadius: 8,
    shadowOffset: { width: 0, height: 3 },
  },
  ring: { position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0, borderWidth: 3, borderRadius: 14 },
  controls: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    backgroundColor: "rgba(0,0,0,0.55)",
    alignItems: "center",
    justifyContent: "center",
    gap: 10,
  },
  button: { backgroundColor: "rgba(255,255,255,0.18)" },
});
