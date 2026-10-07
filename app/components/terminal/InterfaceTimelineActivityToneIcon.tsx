import React from "react";
import { StyleSheet, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { ACTIVITY_HEADER_ICON_SLOT } from "./activityHeaderTextMetrics";
import type {
  TimelineActivityIconName,
  ZenActivityTimelineItem,
} from "./InterfaceTimelineActivityTypes";

interface InterfaceTimelineActivityToneIconProps {
  icon: TimelineActivityIconName;
  activityKind?: ZenActivityTimelineItem["activityKind"];
  color: string;
}

/**
 * Leading tool mark: the tool's bare glyph in the shared 18 pt slot. State
 * lives in the row's StatusMark, so the glyph stays soft ink and tool rows
 * read as quiet margin annotations.
 */
export function InterfaceTimelineActivityToneIcon({
  icon,
  activityKind,
  color,
}: InterfaceTimelineActivityToneIconProps) {
  return (
    <View style={styles.slot}>
      <Ionicons
        name={icon}
        size={activityKind === "reasoning" ? 14 : 13}
        color={color}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  slot: {
    width: ACTIVITY_HEADER_ICON_SLOT,
    height: ACTIVITY_HEADER_ICON_SLOT,
    alignItems: "center",
    justifyContent: "center",
  },
});
