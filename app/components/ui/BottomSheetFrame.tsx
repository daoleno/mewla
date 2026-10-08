import React, { useCallback, useEffect, useMemo } from "react";
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  type StyleProp,
  StyleSheet,
  View,
  type ViewStyle,
} from "react-native";
import Animated, {
  runOnJS,
  useSharedValue,
  useAnimatedStyle,
  withSpring,
  withTiming,
  Easing,
} from "react-native-reanimated";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Radii, useAppColors } from "../../constants/tokens";
import { Spring } from "../../constants/motion";
import { GlassSurface } from "./GlassSurface";
import { useDesktopWeb } from "../navigation/useDesktopWeb";

const AnimatedPressable = Animated.createAnimatedComponent(Pressable);
const DISMISS_DISTANCE = 96;
const SHEET_GUTTER = 8;
// The phone sheet's own maximum, so every sheet keeps its width as a dialog.
const DESKTOP_DIALOG_WIDTH = 720;

interface BottomSheetFrameProps {
  visible: boolean;
  children: React.ReactNode;
  maxHeight?: `${number}%` | number;
  rootStyle?: StyleProp<ViewStyle>;
  cardStyle?: StyleProp<ViewStyle>;
  contentStyle?: StyleProp<ViewStyle>;
  keyboardAvoiding?: boolean;
  /** Pull the grabber down to close. On by default. */
  dragToDismiss?: boolean;
  /** Desktop web: the dialog's width. */
  desktopWidth?: number;
  onClose(): void;
}

/**
 * The shared bottom sheet: a floating glass card inset from the screen edges
 * and the home indicator, springing up over a dimmed backdrop. On desktop web
 * it is a centered dialog without a grabber; Esc and the backdrop close it.
 */
export function BottomSheetFrame({
  visible,
  children,
  maxHeight = "75%",
  rootStyle,
  cardStyle,
  contentStyle,
  keyboardAvoiding = false,
  dragToDismiss: dragToDismissOnPhone = true,
  desktopWidth = DESKTOP_DIALOG_WIDTH,
  onClose,
}: BottomSheetFrameProps) {
  const colors = useAppColors();
  const dialog = useDesktopWeb();
  const dragToDismiss = dragToDismissOnPhone && !dialog;
  const insets = useSafeAreaInsets();
  const progress = useSharedValue(0);
  const dragY = useSharedValue(0);

  useEffect(() => {
    if (visible) {
      dragY.value = 0;
      progress.value = withSpring(1, Spring.rise);
    } else {
      progress.value = withTiming(0, {
        duration: 180,
        easing: Easing.out(Easing.ease),
      });
    }
  }, [dragY, visible, progress]);

  const backdropStyle = useAnimatedStyle(() => ({
    opacity: progress.value * (1 - Math.min(dragY.value / 400, 0.6)),
  }));

  const cardStyleAnim = useAnimatedStyle(() =>
    dialog
      ? {
          transform: [{ scale: 0.97 + progress.value * 0.03 }],
          opacity: Math.min(1, progress.value * 1.6),
        }
      : {
          transform: [{ translateY: (1 - progress.value) * 48 + dragY.value }],
          opacity: Math.min(1, progress.value * 1.6),
        },
  );

  const finishDragClose = useCallback(() => onClose(), [onClose]);
  const dragGesture = useMemo(
    () =>
      Gesture.Pan()
        .enabled(dragToDismiss)
        .activeOffsetY(10)
        .failOffsetX([-24, 24])
        .onUpdate((event) => {
          // Rubber-band upward pulls; follow the finger downward.
          dragY.value =
            event.translationY >= 0
              ? event.translationY
              : -Math.sqrt(-event.translationY) * 2;
        })
        .onEnd((event) => {
          if (dragY.value > DISMISS_DISTANCE || event.velocityY > 900) {
            runOnJS(finishDragClose)();
            return;
          }
          dragY.value = withSpring(0, Spring.rise);
        })
        .onFinalize(() => {
          if (dragY.value <= DISMISS_DISTANCE) {
            dragY.value = withSpring(0, Spring.rise);
          }
        }),
    [dragToDismiss, dragY, finishDragClose],
  );
  const grabber = (
    <View style={styles.grabberTarget}>
      <View style={[styles.grabber, { backgroundColor: colors.borderStrong }]} />
    </View>
  );

  // A fixed card height sizes the animated slot; the glass card fills it.
  const { height: fixedHeight, ...cardOverrides } = StyleSheet.flatten(cardStyle) ?? {};
  const card = (
    <Animated.View
      style={[
        styles.cardSlot,
        dialog
          ? { maxHeight: "85%", height: fixedHeight, maxWidth: desktopWidth }
          : {
              maxHeight,
              height: fixedHeight,
              marginBottom: Math.max(insets.bottom, SHEET_GUTTER),
            },
        cardStyleAnim,
      ]}
    >
      <GlassSurface
        material="thick"
        radius={Radii.sheet}
        elevation="float"
        accessibilityViewIsModal
        style={[
          styles.card,
          dialog && styles.dialogCard,
          fixedHeight != null && styles.cardFill,
          cardOverrides,
        ]}
      >
        {dialog ? null : dragToDismiss ? (
          <GestureDetector gesture={dragGesture}>{grabber}</GestureDetector>
        ) : (
          grabber
        )}
        <View style={[styles.content, contentStyle]}>{children}</View>
      </GlassSurface>
    </Animated.View>
  );
  const body = (
    <>
      <AnimatedPressable
        accessibilityRole="button"
        accessibilityLabel="Close"
        style={[
          styles.backdrop,
          { backgroundColor: colors.modalBackdrop },
          backdropStyle,
        ]}
        onPress={onClose}
      />
      {card}
    </>
  );

  if (!visible) return null;

  return (
    <Modal
      visible
      transparent
      statusBarTranslucent
      navigationBarTranslucent
      animationType="none"
      onRequestClose={onClose}
    >
      {keyboardAvoiding ? (
        <KeyboardAvoidingView
          style={[styles.root, dialog && styles.dialogRoot, rootStyle]}
          behavior={Platform.OS === "ios" ? "padding" : "height"}
        >
          {body}
        </KeyboardAvoidingView>
      ) : (
        <View style={[styles.root, dialog && styles.dialogRoot, rootStyle]}>{body}</View>
      )}
    </Modal>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    justifyContent: "flex-end",
  },
  dialogRoot: {
    justifyContent: "center",
    paddingVertical: 32,
  },
  dialogCard: {
    paddingTop: 18,
  },
  backdrop: {
    ...StyleSheet.absoluteFill,
  },
  cardSlot: {
    width: "100%",
    maxWidth: 720,
    alignSelf: "center",
    paddingHorizontal: SHEET_GUTTER,
  },
  card: {
    flexShrink: 1,
    paddingHorizontal: 18,
    paddingBottom: 20,
    overflow: "hidden",
  },
  cardFill: {
    flex: 1,
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
