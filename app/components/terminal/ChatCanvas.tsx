import React, { type ReactNode } from "react";
import { StyleSheet, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContentWidth } from "../../constants/tokens";

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
export const CHAT_READING_WIDTH = ContentWidth.reading;
/**
 * A Session's frame: header, chat or terminal grid, and composer, one width
 * in both views (~150 terminal columns). Message text caps its own width.
 */
export const SESSION_FRAME_WIDTH = ContentWidth.wide;

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
