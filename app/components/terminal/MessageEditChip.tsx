import React from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { Icon } from "../icons/Icon";
import type { MessageEditMode } from "./messageActionsModel";

/**
 * The cue that the composer holds an edit. Rewinding shows what really
 * happens in pictures: the conversation steps back (undo), the files do not
 * (a struck-through document). A copy shows only the pencil.
 */
export function MessageEditCue({
  mode,
  chrome,
}: {
  mode: MessageEditMode;
  chrome: TerminalThemeChrome;
}) {
  if (mode === "copy") {
    return <Icon name="edit" size={15} color={chrome.accent} />;
  }
  return (
    <View style={styles.cue}>
      <Icon name="undo" size={15} color={chrome.accent} />
      <Icon name="chat" size={14} color={chrome.accent} />
      <View style={styles.untouched}>
        <Icon name="document" size={14} color={chrome.textMuted} />
        <View style={[styles.strike, { backgroundColor: chrome.textMuted }]} />
      </View>
    </View>
  );
}

export function MessageEditChip({
  mode,
  original,
  error,
  busy,
  chrome,
  onCancel,
}: {
  mode: MessageEditMode;
  original: string;
  error?: string;
  busy: boolean;
  chrome: TerminalThemeChrome;
  onCancel(): void;
}) {
  const label =
    mode === "rewind"
      ? "Editing a message. The conversation steps back to it; files stay as they are."
      : "Editing a copy. It is sent as a new message.";
  return (
    <View
      accessible
      accessibilityLabel={error ? `${label} ${error}` : label}
      style={[styles.chip, { backgroundColor: chrome.surface, borderColor: error ? chrome.danger : chrome.border }]}
    >
      <MessageEditCue mode={mode} chrome={chrome} />
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
  cue: { flexDirection: "row", alignItems: "center", gap: 3 },
  untouched: { width: 14, height: 14, alignItems: "center", justifyContent: "center" },
  strike: {
    position: "absolute",
    width: 18,
    height: 1.5,
    borderRadius: 1,
    transform: [{ rotate: "-45deg" }],
  },
  text: { flex: 1, minWidth: 0, fontFamily: Typography.uiFont, fontSize: 13, lineHeight: 18 },
  close: { width: 24, height: 24, alignItems: "center", justifyContent: "center" },
});
