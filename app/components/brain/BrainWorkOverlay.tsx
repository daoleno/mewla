import React, {
  useCallback,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useSyncExternalStore,
} from "react";
import {
  AccessibilityInfo,
  BackHandler,
  Keyboard,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
  findNodeHandle,
} from "react-native";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Animated, {
  Easing,
  runOnJS,
  useAnimatedProps,
  useAnimatedStyle,
  useSharedValue,
  withSpring,
  withTiming,
} from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Spring } from "../../constants/motion";
import { Radii, useAppColors } from "../../constants/tokens";
import { TerminalThemeScope } from "../../theme/provider";
import { GlassSurface } from "../ui/GlassSurface";
import { BrainWorkSheetBody } from "./BrainWorkPanel";
import { brainWorkOverlay, type BrainWorkOverlayContent } from "./brainWorkOverlayStore";

const DISMISS_DISTANCE = 96;
const SHEET_GUTTER = 8;
const SHEET_MAX_WIDTH = 720;
const OPEN_MS = 260;
const CLOSE_MS = 200;
/** The sheet's shared value passes this on its way up: touches belong to it from then on. */
const TOUCHABLE_FROM = 0.05;

function useOverlayContent(): BrainWorkOverlayContent | null {
  return useSyncExternalStore(brainWorkOverlay.subscribeContent, brainWorkOverlay.getContent, brainWorkOverlay.getContent);
}

/** Whether the Work sheet is open: the one flag the rest of the app reads. */
export function useBrainWorkOverlayOpen(): boolean {
  return useSyncExternalStore(brainWorkOverlay.subscribeOpen, brainWorkOverlay.getOpen, brainWorkOverlay.getOpen);
}

/**
 * The rest of the app while the Work sheet is open: hidden from screen readers
 * (an in-tree sheet has no window of its own to do that), and not reached by
 * a keyboard's Tab on web. Children are the caller's elements, so a flip
 * re-renders this wrapper and nothing below it.
 */
export function HideWhenWorkOpen({ children }: { children: React.ReactNode }) {
  const open = useBrainWorkOverlayOpen();
  return (
    <View
      style={styles.fill}
      importantForAccessibility={open ? "no-hide-descendants" : "auto"}
      accessibilityElementsHidden={open}
    >
      {children}
    </View>
  );
}

/**
 * The phone's Work sheet, mounted once at the root over the tab bar and
 * header. While Brain publishes Work it stays mounted and keeps its slips
 * drawn (at low priority, memoised) even when shut, so opening is a shared
 * value moving on the UI thread: no React commit sits between the press and
 * the sheet. A hidden RN Modal would unmount its content; this is why the
 * sheet is not one.
 */
export function BrainWorkOverlayHost() {
  const content = useOverlayContent();
  if (!content) return null;
  return (
    <TerminalThemeScope chrome={content.chrome}>
      <BrainWorkOverlay content={content} />
    </TerminalThemeScope>
  );
}

function BrainWorkOverlay({ content }: { content: BrainWorkOverlayContent }) {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  const { height } = useWindowDimensions();
  const open = useBrainWorkOverlayOpen();
  const progress = useSharedValue(0);
  const dragY = useSharedValue(0);
  const titleRef = useRef<React.ComponentRef<typeof Text>>(null);

  // The slips are drawn at low priority: the first render leaves the body out and a deferred one draws it
  // (time-sliced, so it yields to the chat), and the same again when the Work changes. A press that comes
  // before that draws them at once.
  const deferred = useDeferredValue<BrainWorkOverlayContent | null>(content, null);
  const drawn = deferred ?? (open ? content : null);

  useEffect(
    () =>
      brainWorkOverlay.attach({
        show() {
          dragY.value = 0;
          progress.value = withTiming(1, { duration: OPEN_MS, easing: Easing.bezier(0.16, 1, 0.3, 1) });
        },
        hide() {
          progress.value = withTiming(0, { duration: CLOSE_MS, easing: Easing.in(Easing.quad) });
        },
      }),
    [dragY, progress],
  );

  // Back, Esc and focus: what an RN Modal did for free.
  useEffect(() => {
    if (!open) return;
    Keyboard.dismiss();
    // Android's back button; there is none on iOS, and web's BackHandler throws.
    const back =
      Platform.OS === "android"
        ? BackHandler.addEventListener("hardwareBackPress", () => {
            brainWorkOverlay.close();
            return true;
          })
        : null;
    let esc: ((event: KeyboardEvent) => void) | null = null;
    if (Platform.OS === "web" && typeof document !== "undefined") {
      esc = (event) => {
        if (event.key === "Escape") brainWorkOverlay.close();
      };
      document.addEventListener("keydown", esc);
    }
    const focus = setTimeout(() => {
      const tag = Platform.OS === "web" ? null : findNodeHandle(titleRef.current);
      if (tag) AccessibilityInfo.setAccessibilityFocus(tag);
    }, 120);
    return () => {
      back?.remove();
      if (esc) document.removeEventListener("keydown", esc);
      clearTimeout(focus);
    };
  }, [open]);

  const rootProps = useAnimatedProps(() => ({
    pointerEvents: progress.value > TOUCHABLE_FROM ? ("box-none" as const) : ("none" as const),
  }));
  const backdropStyle = useAnimatedStyle(() => ({
    opacity: progress.value * (1 - Math.min(dragY.value / 400, 0.6)),
  }));
  // The shut sheet waits below the screen, not at opacity 0: nothing of it can be hit or drawn there,
  // and opening is the card rising into view.
  const cardStyle = useAnimatedStyle(() => ({
    transform: [{ translateY: (1 - progress.value) * (height + SHEET_GUTTER) + dragY.value }],
  }));

  const finishDragClose = useCallback(() => {
    brainWorkOverlay.settleClosed();
  }, []);
  const dragGesture = useMemo(
    () =>
      Gesture.Pan()
        .activeOffsetY(10)
        .failOffsetX([-24, 24])
        .onUpdate((event) => {
          // Rubber-band upward pulls; follow the finger downward.
          dragY.value = event.translationY >= 0 ? event.translationY : -Math.sqrt(-event.translationY) * 2;
        })
        .onEnd((event) => {
          if (dragY.value > DISMISS_DISTANCE || event.velocityY > 900) {
            progress.value = withTiming(0, { duration: 160, easing: Easing.out(Easing.ease) });
            runOnJS(finishDragClose)();
            return;
          }
          dragY.value = withSpring(0, Spring.rise);
        })
        .onFinalize(() => {
          if (dragY.value <= DISMISS_DISTANCE) dragY.value = withSpring(0, Spring.rise);
        }),
    [dragY, finishDragClose, progress],
  );

  const close = brainWorkOverlay.close;
  return (
    <Animated.View
      style={styles.root}
      // On native the shared value flips touches at once, before the flag below commits; web has no UI
      // thread, and animated props there turn into a stuck inline style, so it follows the flag only.
      pointerEvents={open ? "box-none" : "none"}
      animatedProps={Platform.OS === "web" ? undefined : rootProps}
      importantForAccessibility={open ? "yes" : "no-hide-descendants"}
      accessibilityElementsHidden={!open}
      accessibilityViewIsModal={open}
    >
      <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: colors.modalBackdrop }, backdropStyle]}>
        {/* Shut, nothing here takes a touch (a web button keeps pointer events its parents turned off). */}
        {open ? <Pressable accessibilityRole="button" accessibilityLabel="Close" style={StyleSheet.absoluteFill} onPress={close} /> : null}
      </Animated.View>
      <View style={[styles.slotRoom, { paddingLeft: insets.left, paddingRight: insets.right }]} pointerEvents="box-none">
        <Animated.View style={[styles.cardSlot, { marginBottom: Math.max(insets.bottom, SHEET_GUTTER) }, cardStyle]}>
          <GlassSurface
              material="thick"
              radius={Radii.sheet}
              elevation="float"
              style={[styles.card, { backgroundColor: content.chrome.appBackground }]}
            >
              <GestureDetector gesture={dragGesture}>
                <View style={styles.grabberTarget}>
                  <View style={[styles.grabber, { backgroundColor: colors.borderStrong }]} />
                </View>
              </GestureDetector>
              <View style={styles.content}>
                {drawn ? (
                  <BrainWorkSheetBody
                    surface={drawn.surface}
                    objective={drawn.objective}
                    chrome={drawn.chrome}
                    onClose={close}
                    onOpenSlip={drawn.onOpenSlip}
                    actionsFor={drawn.actionsFor}
                    refresh={drawn.refresh}
                    titleRef={titleRef}
                  />
                ) : null}
              </View>
            </GlassSurface>
        </Animated.View>
      </View>
    </Animated.View>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  root: {
    ...StyleSheet.absoluteFill,
    overflow: "hidden",
    zIndex: 1000,
    elevation: 1000,
  },
  slotRoom: {
    flex: 1,
    justifyContent: "flex-end",
  },
  cardSlot: {
    width: "100%",
    maxWidth: SHEET_MAX_WIDTH,
    maxHeight: "85%",
    alignSelf: "center",
    paddingHorizontal: SHEET_GUTTER,
  },
  card: {
    flexShrink: 1,
    paddingHorizontal: 18,
    paddingBottom: 20,
    overflow: "hidden",
  },
  content: {
    flexShrink: 1,
  },
  grabberTarget: {
    height: 22,
    alignItems: "center",
    justifyContent: "center",
  },
  grabber: {
    width: 36,
    height: 5,
    borderRadius: 3,
    opacity: 0.6,
  },
});
