import React from "react";
import { StyleSheet, View } from "react-native";
import { StatusBar } from "expo-status-bar";
import { useDesktopWeb } from "../../navigation/useDesktopWeb";
import { SafeAreaView } from "react-native-safe-area-context";
import { TerminalTopBar } from "../TerminalTopBar";
import type { TerminalTopBarProps } from "../TerminalTopBar";
import { TerminalViewport } from "../TerminalViewport";
import type { TerminalViewportProps } from "../TerminalViewport";
import type { TerminalThemeChrome } from "../../../constants/terminalThemes";
import { TerminalScreenOverlays } from "./TerminalScreenOverlays";
import { SESSION_FRAME_WIDTH } from "../ChatCanvas";
import type { TerminalScreenOverlaysProps } from "./TerminalScreenOverlays";

interface TerminalScreenLayoutProps {
  chrome: TerminalThemeChrome;
  statusBarStyle: "auto" | "inverted" | "light" | "dark";
  topBarProps: TerminalTopBarProps;
  viewportProps: TerminalViewportProps;
  overlayProps: TerminalScreenOverlaysProps;
}

export function TerminalScreenLayout({
  chrome,
  statusBarStyle,
  topBarProps,
  viewportProps,
  overlayProps,
}: TerminalScreenLayoutProps) {
  const floatingChatChrome = viewportProps.showInterfaceChat;
  const desktopWeb = useDesktopWeb();
  // Desktop: the flat header's bar spans the pane; its row is the frame.
  const barStyle = desktopWeb
    ? {
        backgroundColor: chrome.appBackground,
        borderBottomWidth: StyleSheet.hairlineWidth,
        borderBottomColor: chrome.border,
      }
    : null;
  return (
    <SafeAreaView
      style={[styles.container, { backgroundColor: chrome.appBackground }]}
      edges={["top"]}
    >
      <StatusBar style={statusBarStyle} />
      {/* One frame for both views of a Session: header, body and composer share
          one width, whether it is the chat or the terminal grid, so switching
          never moves any of them. */}
      <View style={styles.stage}>
        {floatingChatChrome ? (
          <View pointerEvents="box-none" style={[styles.headerOverlay, barStyle]}>
            <View style={styles.headerColumn}>
              <TerminalTopBar {...topBarProps} />
            </View>
          </View>
        ) : (
          <View style={barStyle}>
            <View style={styles.headerColumn}>
              <TerminalTopBar {...topBarProps} />
            </View>
          </View>
        )}
        <View style={styles.frame}>
          <TerminalViewport {...viewportProps} />
        </View>
      </View>

      <TerminalScreenOverlays {...overlayProps} />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  stage: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
  frame: {
    flex: 1,
    minHeight: 0,
    width: "100%",
    maxWidth: SESSION_FRAME_WIDTH,
    alignSelf: "center",
  },
  headerColumn: {
    width: "100%",
    maxWidth: SESSION_FRAME_WIDTH,
    alignSelf: "center",
  },
  headerOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    left: 0,
    zIndex: 20,
  },
});
