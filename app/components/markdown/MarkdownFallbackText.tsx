import React, { useMemo } from "react";
import { Text, type StyleProp, type TextStyle } from "react-native";
import { tokenizeMarkdownLinks } from "./markdownLinks";
import { useMarkdownLinks } from "./useMarkdownLinkOpener";

interface MarkdownFallbackTextProps {
  value: string;
  style?: StyleProp<TextStyle>;
  linkStyle?: StyleProp<TextStyle>;
  baseDir?: string;
  openFile?(reference: string): boolean;
  canOpenFile?(reference: string): boolean;
}

export function MarkdownFallbackText({
  value,
  style,
  linkStyle,
  baseDir,
  openFile,
  canOpenFile,
}: MarkdownFallbackTextProps) {
  const parts = useMemo(() => tokenizeMarkdownLinks(value), [value]);
  const { open: openLink, openable } = useMarkdownLinks({
    baseDir,
    openFile,
    canOpenFile,
  });

  return (
    <Text selectable style={style}>
      {parts.map((part, index) => {
        const url = part.url;
        if (!url || !openable(url)) {
          return part.text;
        }
        return (
          <Text
            accessibilityRole="link"
            key={index}
            onPress={() => openLink(url)}
            style={linkStyle}
          >
            {part.text}
          </Text>
        );
      })}
    </Text>
  );
}
