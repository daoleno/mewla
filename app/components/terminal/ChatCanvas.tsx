import React, { type ReactNode } from "react";
import { StyleSheet, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

type ChatCanvasProps = {
  chrome: TerminalThemeChrome;
  children: ReactNode;
};

export function ChatCanvas({
  chrome,
  children,
}: ChatCanvasProps) {
  return (
    <View style={[styles.root, { backgroundColor: chrome.appBackground }]}>
      <View style={styles.column}>{children}</View>
    </View>
  );
}

/** Conversation leads at a reading width; wide screens get margins, not lines. */
export const CHAT_READING_WIDTH = 820;

const styles = StyleSheet.create({
  root: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
  column: {
    flex: 1,
    minHeight: 0,
    width: "100%",
    maxWidth: CHAT_READING_WIDTH,
    alignSelf: "center",
    position: "relative",
  },
});
