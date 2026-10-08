import React, { useRef } from "react";
import {
  type PressableProps,
  type StyleProp,
  StyleSheet,
  type View,
  type ViewStyle,
} from "react-native";
import * as Haptics from "expo-haptics";
import { ContinuousCorners, useAppTheme } from "../../constants/tokens";
import { AnimatedPressable } from "./AnimatedPressable";
import { Icon, type IconName } from "../icons/Icon";
import { useWebTooltip } from "./useWebTooltip";

type IconButtonTone = "default" | "input" | "ghost" | "tinted";

interface IconButtonProps extends Omit<PressableProps, "style" | "children"> {
  icon: IconName;
  size?: number;
  iconSize?: number;
  color?: string;
  tone?: IconButtonTone;
  /** Light haptic on press. */
  haptic?: boolean;
  /** Web hover text; defaults to the accessibility label. */
  tooltip?: string;
  style?: StyleProp<ViewStyle>;
}

/** Circular icon control; `default` is a thin material capsule. */
export function IconButton({
  icon,
  size = 36,
  iconSize = 18,
  color,
  tone = "default",
  haptic = true,
  disabled,
  style,
  onPress,
  hitSlop,
  tooltip,
  ...props
}: IconButtonProps) {
  const { colors, theme } = useAppTheme();
  const ref = useRef<View>(null);
  useWebTooltip(ref, tooltip ?? props.accessibilityLabel ?? undefined);
  const backgroundColor =
    tone === "input"
      ? colors.inputBackground
      : tone === "ghost"
        ? "transparent"
        : tone === "tinted"
          ? theme.materials.tint
          : theme.materials.thin;
  const borderColor =
    tone === "ghost" || tone === "tinted" ? "transparent" : theme.materials.stroke;
  const iconColor =
    color ?? (tone === "tinted" ? colors.accentStrong : colors.textSecondary);
  const slop = hitSlop ?? Math.max(0, Math.ceil((44 - size) / 2));

  return (
    <AnimatedPressable
      {...props}
      ref={ref}
      accessibilityRole={props.accessibilityRole ?? "button"}
      hitSlop={slop}
      preset="press"
      scale={0.9}
      disabled={disabled}
      onPress={(e) => {
        if (haptic && !disabled) {
          void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
        }
        onPress?.(e);
      }}
      style={[
        styles.button,
        {
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor,
          borderColor,
        },
        style,
      ]}
    >
      <Icon name={icon} size={iconSize} color={iconColor} />
    </AnimatedPressable>
  );
}

const styles = StyleSheet.create({
  button: {
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
});
