import React, { useMemo } from "react";
import { StyleSheet, View } from "react-native";
import { buildChatChrome } from "../../theme";
import { useAppTheme } from "../../constants/tokens";
import { MessageBody } from "../terminal/InterfaceMessageBody";

/** Work Markdown shares the Session reply renderer: tables, task lists, links, images and diagrams. */
export function MarkdownView({ value }: { value: string }) {
  const { theme: appTheme } = useAppTheme();
  const { chrome, theme } = useMemo(() => buildChatChrome(appTheme), [appTheme]);
  return (
    <View style={styles.root}>
      <MessageBody value={value} chrome={chrome} theme={theme} />
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    paddingHorizontal: 18,
    paddingTop: 10,
    paddingBottom: 26,
  },
});
