import React, { useEffect } from "react";
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  StyleSheet,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import Animated, {
  useSharedValue,
  useAnimatedStyle,
  withSpring,
  withTiming,
  Easing,
  interpolate,
} from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Spring } from "../../constants/motion";
import { ContinuousCorners, Radii, shadow, useAppColors } from "../../constants/tokens";
import { useDesktopWeb } from "../navigation/useDesktopWeb";

const AnimatedPressable = Animated.createAnimatedComponent(Pressable);

/**
 * A reusable centered overlay: the dimmed backdrop fades in while the card
 * rises with a calm spring — the native "dialog appears" feel. Used by context
 * menus, rename dialogs, and any centered overlay so they share one entrance.
 *
 * Manages its own Modal; `visible` drives a spring-in and a timed fade-out.
 * Set `avoidKeyboard` for the rename-style dialog that must lift above the
 * keyboard.
 */
export interface RisingSheetProps {
  visible: boolean;
  onClose: () => void;
  cardStyle?: StyleProp<ViewStyle>;
  /** Vertical travel in px for the rise. Default 18. */
  rise?: number;
  align?: "center" | "bottom";
  /**
   * `card` is the centered/bottom dialog. `fullscreen` fills the same Modal
   * owner (for in-flow scanner content) without nesting a second Modal.
   */
  layout?: "card" | "fullscreen";
  avoidKeyboard?: boolean;
  children?: React.ReactNode;
}

export function RisingSheet({
  visible,
  onClose,
  cardStyle,
  rise = 18,
  align = "center",
  layout = "card",
  avoidKeyboard = false,
  children,
}: RisingSheetProps) {
  const colors = useAppColors();
  const desktop = useDesktopWeb();
  const insets = useSafeAreaInsets();
  // Beside a foldable's rail, not under it; backdrops still cover the window.
  const sideInsets = { paddingLeft: insets.left, paddingRight: insets.right };
  const progress = useSharedValue(0);

  useEffect(() => {
    if (visible) {
      progress.value = 0;
      progress.value = withSpring(1, Spring.rise);
    } else {
      progress.value = withTiming(0, {
        duration: 160,
        easing: Easing.out(Easing.ease),
      });
    }
  }, [visible, progress]);

  const backdropStyle = useAnimatedStyle(() => ({ opacity: progress.value }));

  const cardAnimStyle = useAnimatedStyle(() => ({
    opacity: interpolate(progress.value, [0, 0.5, 1], [0, 0.5, 1]),
    transform: [{ translateY: interpolate(progress.value, [0, 1], [rise, 0]) }],
  }));

  const alignStyle = align === "bottom" ? styles.bottom : styles.center;

  // Desktop web: a full-window sheet would cover the sidebar and stretch its
  // fields across the window, so it is a dialog like every other sheet.
  if (layout === "fullscreen" && desktop) {
    return (
      <Modal visible={visible} transparent animationType="none" onRequestClose={onClose}>
        <View style={[styles.root, styles.center, styles.dialogRoot]}>
          <AnimatedPressable
            accessibilityLabel="Close"
            style={[styles.backdrop, { backgroundColor: colors.modalBackdrop }, backdropStyle]}
            onPress={onClose}
          />
          <Animated.View
            style={[
              styles.dialog,
              { borderColor: colors.border, ...shadow("float", colors.shadowColor) },
              cardAnimStyle,
              cardStyle,
            ]}
          >
            {children}
          </Animated.View>
        </View>
      </Modal>
    );
  }

  if (layout === "fullscreen") {
    return (
      <Modal
        visible={visible}
        transparent
        animationType="none"
        onRequestClose={onClose}
      >
        <Animated.View style={[styles.root, sideInsets, cardAnimStyle, cardStyle]}>
          {children}
        </Animated.View>
      </Modal>
    );
  }

  const renderInner = () => (
    <View style={styles.root}>
      <AnimatedPressable
        style={[
          styles.backdrop,
          { backgroundColor: colors.modalBackdrop },
          backdropStyle,
        ]}
        onPress={onClose}
      />
      <View style={[styles.cardSlot, alignStyle, sideInsets]}>
        <Animated.View style={[cardAnimStyle, cardStyle]}>
          {children}
        </Animated.View>
      </View>
    </View>
  );

  return (
    <Modal
      visible={visible}
      transparent
      animationType="none"
      onRequestClose={onClose}
    >
      {avoidKeyboard ? (
        <KeyboardAvoidingView
          style={styles.root}
          behavior={Platform.OS === "ios" ? "padding" : "height"}
        >
          <AnimatedPressable
            style={[
              styles.backdrop,
              { backgroundColor: colors.modalBackdrop },
              backdropStyle,
            ]}
            onPress={onClose}
          />
          <View style={[styles.cardSlot, alignStyle, sideInsets]}>
            <Animated.View style={[cardAnimStyle, cardStyle]}>
              {children}
            </Animated.View>
          </View>
        </KeyboardAvoidingView>
      ) : (
        renderInner()
      )}
    </Modal>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
  },
  backdrop: {
    ...StyleSheet.absoluteFill,
  },
  cardSlot: {
    flex: 1,
    paddingHorizontal: 12,
  },
  center: {
    justifyContent: "center",
  },
  bottom: {
    justifyContent: "flex-end",
    paddingBottom: 32,
  },
  dialogRoot: {
    alignItems: "center",
    padding: 32,
  },
  // BottomSheetFrame's dialog width, at a fixed height so flex content fills it.
  dialog: {
    width: "100%",
    maxWidth: 720,
    height: "85%",
    maxHeight: 760,
    borderRadius: Radii.sheet,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
    ...ContinuousCorners,
  },
});
