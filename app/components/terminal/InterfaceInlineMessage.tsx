import React from "react";
import { StyleSheet, Text } from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { MarkdownInlinePart } from "../markdown/markdownInline";
import { useMarkdownLinkOpener } from "../markdown/useMarkdownLinkOpener";
import { recognizeSessionFileReference } from "../../services/sessionFilePreview";
import { interfaceInlineCodeStyle } from "./InterfaceInlineCodeStyle";
import { tokenizeInlineMessage } from "./InterfaceMessageBodyModel";
import { useSessionFilePreviewContext } from "./SessionFilePreviewContext";

interface InterfaceInlineMessageProps {
  text: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
}

export function InterfaceInlineMessage({
  text,
  chrome,
  theme,
  compact = false,
}: InterfaceInlineMessageProps) {
  const filePreview = useSessionFilePreviewContext();
  const handleLinkPress = useMarkdownLinkOpener();
  const render = (parts: MarkdownInlinePart[]): React.ReactNode =>
    parts.map((part, index) => {
      const content = part.children ? render(part.children) : part.text;
      if (part.kind === "bold") {
        return (
          <Text key={index} style={[styles.messageBold, { color: chrome.text }]}>
            {content}
          </Text>
        );
      }
      if (part.kind === "italic") {
        return (
          <Text key={index} style={[styles.messageItalic, { color: chrome.text }]}>
            {content}
          </Text>
        );
      }
      if (part.kind === "strike") {
        return (
          <Text
            key={index}
            style={[
              styles.messageStrike,
              {
                color: chrome.textMuted,
                textDecorationColor: chrome.textSubtle,
              },
            ]}
          >
            {content}
          </Text>
        );
      }
      if (part.kind === "code") {
        const reference = recognizeSessionFileReference(part.text);
        return (
          <Text
            key={index}
            accessibilityRole={reference ? "link" : undefined}
            onPress={
              reference && filePreview
                ? () => filePreview.open(reference)
                : undefined
            }
            style={[
              styles.messageInlineCode,
              compact ? styles.messageInlineCodeCompact : null,
              {
                color: interfaceInlineCodeStyle(theme, compact).color,
              },
            ]}
          >
            {part.text}
          </Text>
        );
      }
      if (part.kind === "link") {
        const url = part.url;
        return (
          <Text
            accessibilityRole="link"
            key={index}
            onPress={url ? () => handleLinkPress(url) : undefined}
            style={[styles.messageLink, { color: chrome.link }]}
          >
            {content}
          </Text>
        );
      }
      return part.text;
    });

  return <>{render(tokenizeInlineMessage(text))}</>;
}

const styles = StyleSheet.create({
  messageBold: {
    fontFamily: Typography.chatFontMedium,
    letterSpacing: 0,
  },
  messageItalic: {
    fontFamily: Typography.chatFont,
    fontStyle: "italic",
    letterSpacing: 0,
  },
  messageStrike: {
    fontFamily: Typography.chatFont,
    textDecorationLine: "line-through",
    letterSpacing: 0,
  },
  messageInlineCode: {
    fontFamily: Typography.chatMonoFont,
    fontSize: 13,
    lineHeight: 20,
    letterSpacing: 0,
  },
  messageInlineCodeCompact: {
    fontSize: 12,
    lineHeight: 18,
  },
  messageLink: {
    fontFamily: Typography.chatFontMedium,
    textDecorationLine: "underline",
    letterSpacing: 0,
  },
});
