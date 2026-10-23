import React, { useMemo } from "react";
import { Pressable, ScrollView, StyleSheet, Text } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale, UiTextMetrics } from "../../constants/tokens";
import { INTERFACE_TIMELINE_HORIZONTAL_INSET } from "../terminal/interfaceTimelineGeometry";

const CHIP_MIN_SIZE = 44;
const CHIP_PADDING = 9;

/**
 * Optional answers to Brain's last message, as one quiet line of text buttons
 * that continues the message in the timeline. Never a second row: a long set
 * scrolls sideways. The visible chip is only its text; the 44 px target is the
 * chip's own padded box, because Android clips hitSlop to the scroll view.
 * A tap sends the chip's text exactly as if the person typed it.
 */
export function QuickReplies({
  chips,
  chrome,
  onSelect,
}: {
  chips: readonly string[];
  chrome: TerminalThemeChrome;
  onSelect(text: string): void;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      keyboardShouldPersistTaps="handled"
      accessibilityRole="toolbar"
      style={styles.scroll}
      contentContainerStyle={styles.content}
    >
      {chips.map((text) => (
        <Pressable
          key={text}
          accessibilityRole="button"
          accessibilityLabel={text}
          onPress={() => onSelect(text)}
          style={styles.chip}
        >
          {({ pressed }) => (
            <Text style={[styles.label, pressed ? styles.labelPressed : null]} numberOfLines={1}>
              {text}
            </Text>
          )}
        </Pressable>
      ))}
    </ScrollView>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    // The line bleeds to the screen edges so a long set scrolls under them,
    // while the first chip's text lines up with the message above.
    scroll: { flexGrow: 0, marginHorizontal: -INTERFACE_TIMELINE_HORIZONTAL_INSET },
    content: { paddingHorizontal: INTERFACE_TIMELINE_HORIZONTAL_INSET - CHIP_PADDING, alignItems: "center" },
    chip: {
      minHeight: CHIP_MIN_SIZE,
      minWidth: CHIP_MIN_SIZE,
      justifyContent: "center",
      alignItems: "center",
      paddingHorizontal: CHIP_PADDING,
    },
    label: { ...UiTextMetrics, ...TypeScale.label, color: chrome.textMuted },
    labelPressed: { color: chrome.text },
  });
}
