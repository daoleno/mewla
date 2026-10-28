import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import { Icon } from "../icons/Icon";

/** Files are over the chat: a quiet dashed frame says where they will go. */
export function InterfaceDropOverlay({ chrome }: { chrome: TerminalThemeChrome }) {
  return (
    <View
      pointerEvents="none"
      accessibilityLiveRegion="polite"
      style={[styles.scrim, { backgroundColor: chrome.appBackground }]}
    >
      <View style={[styles.frame, { borderColor: chrome.accent }]}>
        <Icon name="attach" size={22} color={chrome.accent} />
        <Text style={[styles.label, { color: chrome.text }]}>Drop to attach</Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  scrim: {
    ...StyleSheet.absoluteFill,
    padding: 12,
    opacity: 0.94,
    zIndex: 30,
  },
  frame: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    gap: 10,
    borderWidth: 2,
    borderStyle: "dashed",
    borderRadius: 20,
  },
  label: {
    ...TypeScale.body,
  },
});
