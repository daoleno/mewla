import React from "react";
import { useAppColors } from "../../constants/tokens";
import { IconButton } from "../ui/IconButton";

/** Visible disc; IconButton pads the touch target out to 44pt. */
export const HEADER_BACK_BUTTON_SIZE = 40;
export const HEADER_BACK_ICON_SIZE = 22;

interface HeaderBackButtonProps {
  onPress(): void;
  /** Defaults to "Back"; sheets name their destination instead. */
  accessibilityLabel?: string;
  /** Glyph colour for surfaces with their own palette (chat canvas, sheets). */
  color?: string;
  /**
   * `material` draws the app's thin disc; `bare` lets a host capsule (chat
   * chrome) supply the surface and fills its 44pt slot.
   */
  surface?: "material" | "bare";
}

/** The one Back affordance: an icon-only chevron, the same on every screen. */
export function HeaderBackButton({
  onPress,
  accessibilityLabel = "Back",
  color,
  surface = "material",
}: HeaderBackButtonProps) {
  const colors = useAppColors();
  return (
    <IconButton
      icon="chevron-left"
      size={surface === "bare" ? 44 : HEADER_BACK_BUTTON_SIZE}
      iconSize={HEADER_BACK_ICON_SIZE}
      tone={surface === "bare" ? "ghost" : "default"}
      color={color ?? colors.textPrimary}
      haptic={false}
      accessibilityLabel={accessibilityLabel}
      onPress={onPress}
    />
  );
}

