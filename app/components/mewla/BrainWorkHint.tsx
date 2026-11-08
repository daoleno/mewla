import React, { useMemo } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Animated, { Easing, useAnimatedStyle, useSharedValue, withTiming } from "react-native-reanimated";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale, useAppTheme } from "../../constants/tokens";
import { Icon } from "../icons/Icon";
import type { WorkHintTone } from "./brainCatState";

/** The hint is 32 pt tall; this slop makes the press target 46 pt. */
const HINT_SLOP = { top: 7, bottom: 7, left: 8, right: 16 } as const;

/** How dark the pill goes under the finger: a seal reads it at once, a tint needs less. */
const PRESSED_SHADE = { needs: 0.22, other: 0.12 } as const;
const PRESSED_SCALE = 0.96;
const PRESS_IN_MS = 50;
/**
 * The feedback gesture must never activate. On Android and iOS an activating
 * gesture cancels the JS responder, so a Tap (which activates on lift) would
 * turn the Pressable's press into a cancel and the list would never open. A
 * long press this long never gets there: it begins on touch down, fails on
 * lift or when the finger drifts or a scroll starts, and that is all the
 * feedback needs.
 */
export const FEEDBACK_NEVER_ACTIVATES_MS = 600_000;
const PRESS_OUT_MS = 160;

/**
 * The cat's status beside it, as the way into the Work list. Work that needs
 * you is the seal pill with a chevron, counted Work a tinted pill, and
 * anything else stays as quiet words. Every one of them presses, and the
 * press target is larger than what is drawn.
 *
 * The press shows on the UI thread: a gesture's begin and end drive the
 * shade and the squeeze as worklets, so the pill answers on the frame the
 * finger lands even while the JS thread is busy drawing the chat. The
 * Pressable still owns the press itself (opening the list on release) and
 * the accessibility action.
 */
export function BrainWorkHint({
  label,
  tone,
  chrome,
  onPress,
}: {
  label: string;
  tone: WorkHintTone;
  chrome: TerminalThemeChrome;
  onPress: () => void;
}) {
  const { theme } = useAppTheme();
  const needs = tone === "needs";
  const pill = tone !== "quiet";
  const pressed = useSharedValue(0);
  const shade = needs ? PRESSED_SHADE.needs : PRESSED_SHADE.other;
  const feedback = useMemo(
    () =>
      Gesture.LongPress()
        .minDuration(FEEDBACK_NEVER_ACTIVATES_MS)
        .hitSlop(HINT_SLOP)
        // A scroll or drag that starts on the pill takes the finger; the press state follows.
        .onBegin(() => {
          pressed.value = withTiming(1, { duration: PRESS_IN_MS, easing: Easing.out(Easing.quad) });
        })
        .onFinalize(() => {
          pressed.value = withTiming(0, { duration: PRESS_OUT_MS, easing: Easing.out(Easing.quad) });
        }),
    [pressed],
  );
  const squeeze = useAnimatedStyle(() => ({
    transform: [{ scale: 1 - (1 - PRESSED_SCALE) * pressed.value }],
  }));
  const darken = useAnimatedStyle(() => ({ opacity: shade * pressed.value }));
  return (
    // On touch web, a vertical swipe that starts on the pill still scrolls the chat.
    <GestureDetector gesture={feedback} touchAction="pan-y">
      <Animated.View style={[styles.frame, squeeze]}>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={`Brain: ${label}`}
          accessibilityHint="Shows the Work list"
          accessibilityLiveRegion="polite"
          hitSlop={HINT_SLOP}
          onPress={onPress}
          style={[styles.hint, pill ? styles.pill : null, pill ? { backgroundColor: needs ? chrome.seal : theme.materials.tint } : null]}
        >
          <Animated.View
            pointerEvents="none"
            style={[styles.shade, { backgroundColor: needs ? "#000" : chrome.text }, darken]}
          />
          <Text
            style={[styles.label, { color: needs ? chrome.onSeal : pill ? chrome.text : chrome.textMuted }]}
            numberOfLines={1}
          >
            {label}
          </Text>
          {pill ? (
            <View pointerEvents="none">
              <Icon name="chevron-right" size={14} color={needs ? chrome.onSeal : chrome.textMuted} />
            </View>
          ) : null}
        </Pressable>
      </Animated.View>
    </GestureDetector>
  );
}

const styles = StyleSheet.create({
  frame: {
    flexShrink: 1,
  },
  hint: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    minHeight: 32,
    borderRadius: 16,
  },
  pill: {
    paddingLeft: 12,
    paddingRight: 8,
  },
  shade: {
    ...StyleSheet.absoluteFill,
    borderRadius: 16,
  },
  label: {
    ...TypeScale.label,
    flexShrink: 1,
  },
});
