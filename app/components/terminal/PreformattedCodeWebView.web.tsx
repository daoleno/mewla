import React from "react";
import { ScrollView, StyleSheet, Text } from "react-native";
import { Typography } from "../../constants/tokens";

interface PreformattedCodeWebViewProps {
  text: string;
  color: string;
  compact: boolean;
}

/** The browser lays out preformatted text natively; no embedded document. */
export function PreformattedCodeWebView({
  text,
  color,
  compact,
}: PreformattedCodeWebViewProps) {
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.scrollContent}
    >
      <Text
        selectable
        style={[
          styles.text,
          {
            color,
            fontSize: compact ? 12 : 13,
            lineHeight: compact ? 18 : 20,
            paddingHorizontal: compact ? 10 : 12,
            paddingVertical: compact ? 9 : 10,
          },
        ]}
      >
        {text}
      </Text>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  scrollContent: {
    flexGrow: 1,
    alignItems: "flex-start",
  },
  text: {
    fontFamily: Typography.chatMonoFont,
    // react-native-web maps this to CSS white-space for preformatted rows.
    whiteSpace: "pre",
  } as object,
});
