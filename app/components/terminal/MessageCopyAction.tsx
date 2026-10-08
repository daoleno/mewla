import React, { useEffect, useState } from "react";
import { Pressable, StyleSheet, Text } from "react-native";
import * as Clipboard from "expo-clipboard";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { Icon } from "../icons/Icon";
import { useDesktopWeb } from "../navigation/useDesktopWeb";
import { CODE_BLOCK_COPIED_RESET_MS } from "./InterfaceMessageCodeBlockCopy";

/**
 * Desktop web: a quiet Copy under a finished reply, as in ChatGPT and Claude
 * on the web. Phones copy through text selection, so they get none.
 */
export function MessageCopyAction({
  text,
  chrome,
}: {
  text: string;
  chrome: TerminalThemeChrome;
}) {
  const desktopWeb = useDesktopWeb();
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), CODE_BLOCK_COPIED_RESET_MS);
    return () => clearTimeout(timer);
  }, [copied]);
  if (!desktopWeb || !text.trim()) return null;
  const color = copied ? chrome.accent : chrome.textMuted;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={copied ? "Message copied" : "Copy message"}
      onPress={() => {
        void Clipboard.setStringAsync(text).then(() => setCopied(true));
      }}
      style={(state) => [
        styles.button,
        (state as { hovered?: boolean }).hovered ? { backgroundColor: chrome.surfaceMuted } : null,
      ]}
    >
      <Icon name={copied ? "check" : "copy"} size={14} color={color} />
      <Text accessibilityLiveRegion="polite" style={[styles.label, { color }]}>
        {copied ? "Copied" : "Copy"}
      </Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  button: {
    alignSelf: "flex-start",
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    marginTop: 4,
    marginLeft: -6,
    paddingHorizontal: 6,
    paddingVertical: 3,
    borderRadius: 6,
  },
  label: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 12,
    lineHeight: 16,
  },
});
