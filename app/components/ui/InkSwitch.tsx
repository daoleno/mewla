import React from "react";
import { Switch, type SwitchProps } from "react-native";
import { useAppTheme } from "../../constants/tokens";

/** Switches are ink, like every other control (DESIGN.md: Paper and ink). */
export function InkSwitch(props: SwitchProps) {
  const { colors } = useAppTheme();
  // react-native-web colours an on thumb with activeThumbColor, outside SwitchProps.
  const web = { activeThumbColor: colors.bgSurface, activeTrackColor: colors.accent } as object;
  return <Switch trackColor={{ false: colors.border, true: colors.accent }} thumbColor={colors.bgSurface} {...web} {...props} />;
}
