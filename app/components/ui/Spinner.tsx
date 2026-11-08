import React from "react";
import { ActivityIndicator, type StyleProp, type ViewStyle } from "react-native";
import { useAppColors } from "../../constants/tokens";

interface SpinnerProps {
  /** Defaults to the quiet tertiary ink; pass the ink of the row it sits in. */
  color?: string;
  style?: StyleProp<ViewStyle>;
}

/**
 * The small in-line busy mark: trailing a row, beside a status word. It is
 * always `small` (the platform default is large on Android) and always themed.
 * Full-screen waits use EmptyState `busy`; a busy Button or IconButton draws
 * its own.
 */
export function Spinner({ color, style }: SpinnerProps) {
  const colors = useAppColors();
  return (
    <ActivityIndicator
      size="small"
      color={color ?? colors.textTertiary}
      accessibilityLabel="Loading"
      style={style}
    />
  );
}
