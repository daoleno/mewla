import React, { useMemo } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Radii, TypeScale, UiTextMetrics } from "../../constants/tokens";
import { outlinedSurface } from "../ui/outlinedSurface";

/**
 * Tap-to-answer chips under Brain's last message. Any Brain question can use
 * them; a tap sends the chip's text exactly as if the person typed it.
 */
export function QuickReplies({
  rows,
  chrome,
  onSelect,
}: {
  rows: readonly (readonly string[])[];
  chrome: TerminalThemeChrome;
  onSelect(text: string): void;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  return (
    <View style={styles.wrap} accessibilityRole="toolbar">
      {rows.map((row, index) => (
        <View key={index} style={styles.row}>
          {row.map((text) => (
            <Pressable
              key={text}
              accessibilityRole="button"
              accessibilityLabel={text}
              onPress={() => onSelect(text)}
              style={({ pressed }) => [styles.chip, pressed ? styles.chipPressed : null]}
            >
              <Text style={styles.label} numberOfLines={1}>
                {text}
              </Text>
            </Pressable>
          ))}
        </View>
      ))}
    </View>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    wrap: { marginHorizontal: 10, marginBottom: 6, gap: 8 },
    row: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
    chip: {
      minHeight: 44,
      maxWidth: "100%",
      justifyContent: "center",
      ...outlinedSurface(Radii.pill, chrome.border),
      backgroundColor: chrome.surface,
      paddingHorizontal: 14,
    },
    chipPressed: { backgroundColor: chrome.surfaceActive },
    label: { ...UiTextMetrics, ...TypeScale.label, color: chrome.text, flexShrink: 1 },
  });
}
