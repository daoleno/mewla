import React from "react";
import { Pressable, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import type { PathMark } from "../../services/connectionPath";
import { Icon, type IconName } from "../icons/Icon";

/** Direct is a link; the relay is a plain cloud. Both in the same quiet ink. */
export const PATH_MARK_ICONS: Record<PathMark["kind"], IconName> = {
  direct: "link",
  relay: "cloud",
};

/**
 * How this computer is reached, as one small glyph in tertiary ink: never a
 * warning color, since the relay is fine, only slower. Given `onPress` it is
 * a button of its own, so it must sit beside a pressable row, never inside
 * one; without it the glyph is plain and its row says the path.
 */
export function ConnectionPathMark({
  mark,
  size = 15,
  onPress,
  tabIndex,
}: {
  mark: PathMark;
  size?: number;
  onPress?: () => void;
  tabIndex?: 0 | -1;
}) {
  const { colors } = useAppTheme();
  const glyph = <Icon name={PATH_MARK_ICONS[mark.kind]} size={size} color={colors.textTertiary} />;
  if (!onPress) return <View>{glyph}</View>;
  return (
    <Pressable
      onPress={onPress}
      hitSlop={8}
      tabIndex={tabIndex}
      accessibilityRole="button"
      accessibilityLabel={`Connection, ${mark.label}`}
      accessibilityHint="Shows how this phone reaches the computer"
    >
      {glyph}
    </Pressable>
  );
}
