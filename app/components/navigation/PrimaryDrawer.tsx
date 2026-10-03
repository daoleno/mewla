import React, { useCallback, useMemo, useState, type ReactNode } from "react";
import {
  Keyboard,
  StyleSheet,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import Animated, {
  ReduceMotion,
  runOnJS,
  runOnUI,
  useAnimatedProps,
  useAnimatedStyle,
  useSharedValue,
  withSpring,
  type SharedValue,
} from "react-native-reanimated";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import {
  beginDrawerDrag,
  endDrawerDrag,
  requestDrawerTarget,
  updateDrawerDrag,
  type PrimaryDrawerEffects,
  type PrimaryDrawerMotion,
} from "./primaryDrawerMotion";

/** Horizontal travel before the pan claims the touch from the pager/content. */
const DRAWER_ACTIVATION_DISTANCE = 12;
/** Vertical travel that hands the touch to scrolling instead. */
const DRAWER_VERTICAL_FAIL_DISTANCE = 5;
const OVERLAY_ACTIVE_PROGRESS = 0.05;
// Overdamped and clamped, matching the previous drawer's settle.
const DRAWER_SPRING = {
  stiffness: 1000,
  damping: 500,
  mass: 3,
  overshootClamping: true,
  reduceMotion: ReduceMotion.Never,
} as const;

export interface PrimaryDrawerController {
  motion: PrimaryDrawerMotion;
  /** React's mirror of the UI-thread target; never written by requests. */
  open: boolean;
  /** Ask the UI thread to open or close, whatever JS currently believes. */
  request(open: boolean): void;
  /** Effects bound to `motion`; worklet-safe. */
  effects: PrimaryDrawerEffects;
}

function useMotionCells(): PrimaryDrawerMotion & {
  position: SharedValue<number>;
} {
  const position = useSharedValue(0);
  const target = useSharedValue(0);
  const dragging = useSharedValue(false);
  const dragStart = useSharedValue(0);
  const pending = useSharedValue(-1);
  return useMemo(
    () => ({ position, target, dragging, dragStart, pending }),
    [dragStart, dragging, pending, position, target],
  );
}

/**
 * Owns the primary drawer's open state on the UI thread. `onChange` runs on
 * JS, in order, after every target change, so React can mirror it.
 */
export function usePrimaryDrawerController(
  onChange?: (open: boolean) => void,
): PrimaryDrawerController {
  const motion = useMotionCells();
  const [open, setOpen] = useState(false);
  const mirror = useCallback(
    (target: number) => {
      const next = target === 1;
      setOpen(next);
      onChange?.(next);
    },
    [onChange],
  );
  const effects = useMemo<PrimaryDrawerEffects>(
    () => ({
      settle(target, velocity) {
        "worklet";
        motion.position.value = withSpring(target, {
          ...DRAWER_SPRING,
          velocity,
        });
      },
      notify(target) {
        "worklet";
        runOnJS(mirror)(target);
      },
    }),
    [mirror, motion],
  );
  const request = useCallback(
    (next: boolean) => {
      runOnUI((value: boolean) => {
        "worklet";
        requestDrawerTarget(motion, value, effects);
      })(next);
    },
    [effects, motion],
  );
  return useMemo(
    () => ({ effects, motion, open, request }),
    [effects, motion, open, request],
  );
}

interface PrimaryDrawerProps {
  children: ReactNode;
  controller: PrimaryDrawerController;
  drawer: ReactNode;
  drawerStyle?: StyleProp<ViewStyle>;
  drawerWidth: number;
  /** Leading strip that can start an opening swipe while closed. */
  edgeWidth: number;
  onDragStart?(): void;
  overlayAccessibilityLabel: string;
  overlayColor: string;
  style?: StyleProp<ViewStyle>;
  /** Opening swipe from the edge. Closing swipes work whenever open. */
  swipeToOpenEnabled: boolean;
  windowWidth: number;
}

/**
 * Leading navigation drawer over the primary surface. Every visual is
 * driven by `motion.position`; gestures and taps change `motion.target` on
 * the UI thread so a close can never be lost behind a busy JS thread.
 */
export function PrimaryDrawer({
  children,
  controller,
  drawer,
  drawerStyle,
  drawerWidth,
  edgeWidth,
  onDragStart,
  overlayAccessibilityLabel,
  overlayColor,
  style,
  swipeToOpenEnabled,
  windowWidth,
}: PrimaryDrawerProps) {
  const { effects, motion, open, request } = controller;
  const touchStartX = useSharedValue(0);
  const beginDrag = useCallback(() => {
    Keyboard.dismiss();
    onDragStart?.();
  }, [onDragStart]);

  // Gesture configuration follows React's mirror; the release decision
  // follows the UI-thread target, so a stale mirror cannot misplace it.
  const pan = useMemo(() => {
    const gesture = Gesture.Pan()
      .enabled(open || swipeToOpenEnabled)
      .hitSlop(open ? { left: 0 } : { left: 0, width: edgeWidth })
      .failOffsetY([
        -DRAWER_VERTICAL_FAIL_DISTANCE,
        DRAWER_VERTICAL_FAIL_DISTANCE,
      ])
      .onBegin((event) => {
        touchStartX.value = event.x;
      })
      .onStart(() => {
        beginDrawerDrag(motion);
        runOnJS(beginDrag)();
      })
      .onUpdate((event) => {
        // A closing drag that starts on the backdrop moves the drawer only
        // once the finger reaches its edge.
        const backdropTravel = Math.max(touchStartX.value - drawerWidth, 0);
        const offsetX =
          event.translationX < 0
            ? Math.min(event.translationX + backdropTravel, 0)
            : event.translationX;
        updateDrawerDrag(motion, offsetX, drawerWidth);
      })
      .onFinalize((event) => {
        endDrawerDrag(
          motion,
          event.translationX,
          event.velocityX,
          drawerWidth,
          effects,
        );
      });
    return open
      ? gesture
          .activeOffsetX([-DRAWER_ACTIVATION_DISTANCE, windowWidth])
          .failOffsetX(DRAWER_ACTIVATION_DISTANCE)
      : gesture
          .activeOffsetX([-windowWidth, DRAWER_ACTIVATION_DISTANCE])
          .failOffsetX(-DRAWER_ACTIVATION_DISTANCE);
  }, [
    beginDrag,
    drawerWidth,
    edgeWidth,
    effects,
    motion,
    open,
    swipeToOpenEnabled,
    touchStartX,
    windowWidth,
  ]);

  const backdropTap = useMemo(
    () =>
      Gesture.Tap().onEnd((_event, success) => {
        if (success) {
          requestDrawerTarget(motion, false, effects);
        }
      }),
    [effects, motion],
  );

  const drawerAnimatedStyle = useAnimatedStyle(() => ({
    transform: [{ translateX: (motion.position.value - 1) * drawerWidth }],
    // A layout prop makes Reanimated commit the settled position to the
    // shadow tree, which Pressable uses to measure its press region.
    zIndex: motion.position.value === 1 ? 2 : 3,
  }));
  const overlayAnimatedStyle = useAnimatedStyle(() => ({
    opacity: motion.position.value,
  }));
  const overlayAnimatedProps = useAnimatedProps(() => ({
    pointerEvents:
      motion.position.value > OVERLAY_ACTIVE_PROGRESS
        ? ("auto" as const)
        : ("none" as const),
  }));
  const closeFromAccessibility = useCallback(() => request(false), [request]);

  return (
    <GestureDetector gesture={pan}>
      <Animated.View style={[styles.root, style]}>
        <View style={styles.content}>{children}</View>
        <GestureDetector gesture={backdropTap}>
          <Animated.View
            accessible={open}
            accessibilityElementsHidden={!open}
            importantForAccessibility={open ? "yes" : "no-hide-descendants"}
            accessibilityRole="button"
            accessibilityLabel={overlayAccessibilityLabel}
            accessibilityActions={[{ name: "activate" }]}
            onAccessibilityAction={closeFromAccessibility}
            animatedProps={overlayAnimatedProps}
            style={[
              StyleSheet.absoluteFill,
              { backgroundColor: overlayColor },
              overlayAnimatedStyle,
            ]}
          />
        </GestureDetector>
        <Animated.View
          style={[
            styles.drawer,
            { width: drawerWidth },
            drawerStyle,
            drawerAnimatedStyle,
          ]}
        >
          {drawer}
        </Animated.View>
      </Animated.View>
    </GestureDetector>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    overflow: "hidden",
  },
  content: {
    flex: 1,
  },
  drawer: {
    position: "absolute",
    top: 0,
    bottom: 0,
    left: 0,
    maxWidth: "100%",
  },
});
