import React from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import {
  ACTIVITY_HEADER_ICON_SLOT,
  ACTIVITY_HEADER_ROW_MIN_HEIGHT,
  ACTIVITY_HEADER_TITLE_FONT,
  activityHeaderSharedTextStyle,
} from "./activityHeaderTextMetrics";

/**
 * Plan annotation header. Shares the tool row's tone-mark slot, caption line
 * box and gap so plans and tools hang from one leading rail.
 */
export function ZenPlanHeader({
  accentColor,
  chrome,
}: {
  accentColor: string;
  chrome: TerminalThemeChrome;
}) {
  return (
    <View style={styles.row}>
      <View style={styles.mark}>
        <Ionicons name="checkbox-outline" size={13} color={accentColor} />
      </View>
      <Text
        style={[styles.title, { color: chrome.textMuted }]}
        numberOfLines={1}
      >
        Updated Plan
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    alignSelf: "flex-start",
    minHeight: ACTIVITY_HEADER_ROW_MIN_HEIGHT,
    maxWidth: "100%",
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  mark: {
    width: ACTIVITY_HEADER_ICON_SLOT,
    height: ACTIVITY_HEADER_ICON_SLOT,
    alignItems: "center",
    justifyContent: "center",
  },
  title: {
    ...activityHeaderSharedTextStyle,
    fontFamily: ACTIVITY_HEADER_TITLE_FONT,
    fontWeight: "500",
  },
});
