import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale, useAppTheme } from "../../constants/tokens";
import { Icon } from "../icons/Icon";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import type { WorkHintTone } from "./brainCatState";
import { markWorkOpen } from "./workOpenTiming";

/** The hint is 32 pt tall; this slop makes the press target 46 pt. */
const HINT_SLOP = { top: 7, bottom: 7, left: 8, right: 16 } as const;

/**
 * The cat's status beside it, as the way into the Work list. Work that needs
 * you is the seal pill with a chevron, counted Work a tinted pill, and
 * anything else stays as quiet words. Every one of them presses, and the
 * press target is larger than what is drawn.
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
  return (
    <AnimatedPressable
      accessibilityRole="button"
      accessibilityLabel={`Brain: ${label}`}
      accessibilityHint="Shows the Work list"
      accessibilityLiveRegion="polite"
      hitSlop={HINT_SLOP}
      scale={0.97}
      onPressIn={() => markWorkOpen("pressIn")}
      onPress={() => {
        markWorkOpen("press");
        onPress();
      }}
      style={[styles.hint, pill ? styles.pill : null, pill ? { backgroundColor: needs ? chrome.seal : theme.materials.tint } : null]}
    >
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
    </AnimatedPressable>
  );
}

const styles = StyleSheet.create({
  hint: {
    flexShrink: 1,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    minHeight: 32,
  },
  pill: {
    paddingLeft: 12,
    paddingRight: 8,
    borderRadius: 16,
  },
  label: {
    ...TypeScale.label,
    flexShrink: 1,
  },
});
