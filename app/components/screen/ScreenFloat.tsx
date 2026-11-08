import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Keyboard, Platform, Pressable, StyleSheet, View, useWindowDimensions, type LayoutChangeEvent } from "react-native";
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
import { useAppTheme } from "../../constants/tokens";
import { getScreenFloatCorner, getScreenFloatSize, setScreenFloatCorner, setScreenFloatSize } from "../../services/storage";
import { ActionMenu, AppText, IconButton } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import {
  DEFAULT_FLOAT_CORNER,
  FLOAT_DEFAULT,
  clampFloatFraction,
  floatOrigin,
  floatSize,
  nearestFloatCorner,
  parseFloatCorner,
  parseFloatFraction,
  resizedFloatWidth,
  toggledFloatFraction,
  type FloatCorner,
} from "./floatGeometry";

const EDGE = 12;
/** Under the page header. */
const HEADER = 56;
/** The chat composer, with room for a few lines of draft: the window stays above it. */
const COMPOSER = 140;
const BAR = 30;
const BUTTON = 28;
const RADIUS = 10;
/** The resize handle: a touch area around a small grip in the window's inner corner. */
const HANDLE = 32;
const GRIP = 14;

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

type Gesturing = "none" | "drag" | "resize";

export interface ScreenFloatProps {
  /** Width over height of the picture; the window is exactly that shape under its bar. */
  ratio: number;
  title: string;
  /** How many screens may show here, and which this is; more than one, a tap on the title goes to the next. */
  count: number;
  index: number;
  onCycle(): void;
  menu: readonly ActionMenuItem[];
  /** Opens this screen's full view. */
  onOpen(): void;
  /** Keyboard toggle for a screen the person can type on. */
  onKeyboard?: () => void;
  keyboardOpen?: boolean;
  /** Room under the picture the hosted view needs (its key bar while the system keyboard is up). */
  extraHeight?: number;
  /** The person is typing on the screen with the keyboard up: the chat's composer is not in the way. */
  typing?: boolean;
  /** Something asks for the person: a ring that keeps breathing in the seal colour. */
  attention?: boolean;
  /** Draws the live view into the picture's exact size. */
  children(picture: { width: number; height: number }): React.ReactNode;
}

/**
 * The one floating window for every agent screen: Phone use, an agent's Linux display, the Mac. It owns
 * position, corners, size, the bar and the menu; what it shows is the hosted live view, which is also the
 * input surface, so only the bar drags. Mounted once above navigation by ScreenFloatHost.
 */
export function ScreenFloat({ ratio, title, count, index, onCycle, menu, onOpen, onKeyboard, keyboardOpen, extraHeight = 0, typing, attention, children }: ScreenFloatProps) {
  const { colors } = useAppTheme();
  const insets = useSafeAreaInsets();
  const window = useWindowDimensions();
  const keyboard = useKeyboardHeight();
  const [box, setBox] = useState({ width: window.width, height: window.height });
  const [corner, setCorner] = useState<FloatCorner>(DEFAULT_FLOAT_CORNER);
  const [fraction, setFraction] = useState(FLOAT_DEFAULT);
  const [menuOpen, setMenuOpen] = useState(false);
  const placed = useRef(false);
  const gesturing = useRef<Gesturing>("none");

  useEffect(() => {
    let alive = true;
    void Promise.all([getScreenFloatCorner(), getScreenFloatSize()])
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
      left: insets.left,
      top: insets.top + HEADER,
      right: box.width - insets.right,
      bottom: box.height - (keyboard > 0 ? keyboard : insets.bottom) - (typing ? 0 : COMPOSER),
    }),
    [box.height, box.width, insets.bottom, insets.left, insets.right, insets.top, keyboard, typing],
  );
  const safeRatio = ratio > 0 ? ratio : 9 / 20;
  // The picture has the screen's exact shape, so it fills its place; the window never reaches the composer or the keyboard.
  const picture = useMemo(
    () => floatSize(safeRatio, fraction, box, Math.max(0, area.bottom - area.top - 2 * EDGE - BAR - extraHeight)),
    [area.bottom, area.top, box, extraHeight, fraction, safeRatio],
  );
  const size = useMemo(() => ({ width: picture.width, height: picture.height + BAR + extraHeight }), [extraHeight, picture.height, picture.width]);

  const origin = useMemo(() => floatOrigin(corner, area, size, EDGE), [area, corner, size]);
  const x = useSharedValue(origin.x);
  const y = useSharedValue(origin.y);

  // The gestures are made once and read what changes through this, so a resize that re-renders at
  // every move never replaces the gesture that is in progress.
  const latest = useRef({ area, size, corner, ratio: safeRatio, fraction, box });
  latest.current = { area, size, corner, ratio: safeRatio, fraction, box };

  // The window follows its corner: the keyboard, a rotation or a new size move it, a drag does not.
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
    void setScreenFloatSize(next).catch(() => undefined);
  }, []);

  const start = useSharedValue({ x: 0, y: 0 });
  const startWidth = useRef(0);
  const drag = useMemo(() => {
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
        void setScreenFloatCorner(next).catch(() => undefined);
        const target = floatOrigin(next, l.area, l.size, EDGE);
        x.value = withTiming(target.x, { duration: 220, easing: Easing.out(Easing.cubic) });
        y.value = withTiming(target.y, { duration: 220, easing: Easing.out(Easing.cubic) });
      })
      .onFinalize(() => {
        if (gesturing.current === "drag") gesturing.current = "none";
      });
    // A double tap on the bar goes between the default and the large size.
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
    return Gesture.Race(pan, doubleTap);
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

  // Something asking for the person keeps a ring breathing.
  const pulse = useSharedValue(0);
  useEffect(() => {
    if (!attention) {
      cancelAnimation(pulse);
      pulse.value = 0;
      return;
    }
    pulse.value = withRepeat(
      withSequence(withTiming(1, { duration: 700, easing: Easing.inOut(Easing.quad) }), withTiming(0.2, { duration: 700, easing: Easing.inOut(Easing.quad) })),
      -1,
    );
    return () => cancelAnimation(pulse);
  }, [attention, pulse]);

  const frameStyle = useAnimatedStyle(() => ({ transform: [{ translateX: x.value }, { translateY: y.value }] }));
  const ringStyle = useAnimatedStyle(() => ({ opacity: pulse.value }));

  const onLayout = (event: LayoutChangeEvent) => {
    const { width, height } = event.nativeEvent.layout;
    setBox((previous) => (previous.width === width && previous.height === height ? previous : { width, height }));
  };

  // The handle sits in the corner opposite the one the window is anchored to, so dragging it outward grows the window.
  const handleLeft = corner.endsWith("right");
  const handleTop = corner.startsWith("bottom");
  const cycles = count > 1;

  return (
    <View style={StyleSheet.absoluteFill} pointerEvents="box-none" onLayout={onLayout}>
      <Animated.View
        accessibilityLabel={`Screen of ${title}`}
        style={[styles.frame, { width: size.width, height: size.height, backgroundColor: colors.bgSurface, shadowColor: "#000" }, frameStyle]}
      >
        <GestureDetector gesture={drag}>
          <View style={styles.bar}>
            <Pressable
              accessibilityRole={cycles ? "button" : "text"}
              accessibilityLabel={cycles ? `${title}, ${index + 1} of ${count}, show the next` : title}
              disabled={!cycles}
              onPress={onCycle}
              style={styles.title}
            >
              <AppText variant="caption" numberOfLines={1} style={styles.titleText}>
                {cycles ? `${title}  ${index + 1}/${count}` : title}
              </AppText>
            </Pressable>
            {onKeyboard ? (
              <IconButton icon="keyboard" size={BUTTON} iconSize={15} color="#fff" accessibilityLabel={keyboardOpen ? "Hide keyboard" : "Show keyboard"} onPress={onKeyboard} style={keyboardOpen ? styles.buttonOn : styles.button} />
            ) : null}
            <IconButton icon="more-vertical" size={BUTTON} iconSize={15} color="#fff" accessibilityLabel={`Options for ${title}`} onPress={() => setMenuOpen(true)} style={styles.button} />
            <IconButton icon="expand" size={BUTTON} iconSize={15} color="#fff" accessibilityLabel="Open" onPress={onOpen} style={styles.button} />
          </View>
        </GestureDetector>
        <View style={{ width: picture.width, height: picture.height + extraHeight, backgroundColor: colors.bgElevated }}>{children(picture)}</View>
        <View pointerEvents="none" style={[styles.edge, { borderColor: colors.borderSubtle }]} />
        {attention ? <Animated.View pointerEvents="none" style={[styles.ring, { borderColor: colors.seal }, ringStyle]} /> : null}
        <GestureDetector gesture={handle}>
          <View
            accessibilityLabel="Resize"
            style={[styles.handle, handleLeft ? styles.handleLeft : styles.handleRight, handleTop ? styles.handleTop : styles.handleBottom]}
          >
            <View
              style={[
                styles.grip,
                { borderColor: handleTop ? "rgba(255,255,255,0.9)" : colors.textTertiary },
                handleLeft ? { borderLeftWidth: 3, borderTopLeftRadius: handleTop ? 6 : 0, borderBottomLeftRadius: handleTop ? 0 : 6 } : { borderRightWidth: 3, borderTopRightRadius: handleTop ? 6 : 0, borderBottomRightRadius: handleTop ? 0 : 6 },
                handleTop ? { borderTopWidth: 3 } : { borderBottomWidth: 3 },
              ]}
            />
          </View>
        </GestureDetector>
      </Animated.View>
      <ActionMenu visible={menuOpen} title={title} items={menu} onClose={() => setMenuOpen(false)} />
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
  bar: { height: BAR, flexDirection: "row", alignItems: "center", paddingLeft: 10, paddingRight: 2, gap: 2, backgroundColor: "rgba(24,24,24,0.94)" },
  title: { flex: 1, justifyContent: "center", height: BAR },
  titleText: { color: "#fff" },
  button: { backgroundColor: "transparent" },
  buttonOn: { backgroundColor: "rgba(255,255,255,0.22)" },
  // The hairline is drawn over the picture, so it takes none of the window's size.
  edge: { ...fill, borderWidth: StyleSheet.hairlineWidth, borderRadius: RADIUS },
  ring: { ...fill, borderWidth: 3, borderRadius: RADIUS },
  handle: { position: "absolute", width: HANDLE, height: HANDLE, padding: 8 },
  handleLeft: { left: 0, alignItems: "flex-start" },
  handleRight: { right: 0, alignItems: "flex-end" },
  // Under the bar, so the bar's buttons stay reachable.
  handleTop: { top: BAR, justifyContent: "flex-start" },
  handleBottom: { bottom: 0, justifyContent: "flex-end" },
  grip: { width: GRIP, height: GRIP },
});
