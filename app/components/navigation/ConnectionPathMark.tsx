import React from "react";
import { View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import type { PathMark } from "../../services/connectionPath";
import { Icon, type IconName } from "../icons/Icon";

/** Direct is a link; the relay is the cloud. Both in the same quiet ink. */
export const PATH_MARK_ICONS: Record<PathMark["kind"], IconName> = {
  direct: "link",
  relay: "cloud-upload",
};

/**
 * How this computer is reached, as one small glyph in tertiary ink: never a
 * warning color, since the relay is fine, only slower. It carries no gesture
 * of its own: its row names the path to assistive tech and a long press on
 * the row reveals the detail.
 */
export function ConnectionPathMark({ mark, size = 15 }: { mark: PathMark; size?: number }) {
  const { colors } = useAppTheme();
  return (
    <View>
      <Icon name={PATH_MARK_ICONS[mark.kind]} size={size} color={colors.textTertiary} />
    </View>
  );
}
