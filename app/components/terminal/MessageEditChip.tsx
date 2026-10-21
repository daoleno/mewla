import React from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { Icon } from "../icons/Icon";

/** The composer holds an edit: the message being edited, and a way to cancel. */
export function MessageEditChip({
  original,
  error,
  busy,
  chrome,
  onCancel,
}: {
  original: string;
  error?: string;
  busy: boolean;
  chrome: TerminalThemeChrome;
  onCancel(): void;
}) {
  return (
    <View
      accessible
      accessibilityLabel={error ? `Editing message. ${error}` : "Editing message"}
      style={[styles.chip, { backgroundColor: chrome.surface, borderColor: error ? chrome.danger : chrome.border }]}
    >
      <Icon name="edit" size={15} color={chrome.accent} />
      <Text numberOfLines={1} style={[styles.text, { color: error ? chrome.danger : chrome.textMuted }]}>
        {error ?? original.replace(/\s+/g, " ").trim()}
      </Text>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Cancel edit"
        disabled={busy}
        hitSlop={10}
        onPress={onCancel}
        style={[styles.close, busy ? { opacity: 0.4 } : null]}
      >
        <Icon name="close" size={14} color={chrome.textMuted} />
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  chip: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    marginHorizontal: 12,
    marginBottom: 6,
    paddingLeft: 10,
    paddingRight: 6,
    minHeight: 34,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
  },
  text: { flex: 1, minWidth: 0, fontFamily: Typography.uiFont, fontSize: 13, lineHeight: 18 },
  close: { width: 24, height: 24, alignItems: "center", justifyContent: "center" },
});
