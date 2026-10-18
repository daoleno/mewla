import React, { useMemo } from "react";
import { Text, type StyleProp, type TextStyle } from "react-native";
import { tokenizeMarkdownLinks } from "./markdownLinks";
import { useMarkdownLinkOpener } from "./useMarkdownLinkOpener";

interface MarkdownFallbackTextProps {
  value: string;
  style?: StyleProp<TextStyle>;
  linkStyle?: StyleProp<TextStyle>;
  baseDir?: string;
  openFile?(reference: string): boolean;
}

export function MarkdownFallbackText({
  value,
  style,
  linkStyle,
  baseDir,
  openFile,
}: MarkdownFallbackTextProps) {
  const parts = useMemo(() => tokenizeMarkdownLinks(value), [value]);
  const openLink = useMarkdownLinkOpener({ baseDir, openFile });

  return (
    <Text selectable style={style}>
      {parts.map((part, index) => {
        const url = part.url;
        if (!url) {
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
