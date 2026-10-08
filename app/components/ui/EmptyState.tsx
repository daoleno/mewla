import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import Svg, { Circle } from "react-native-svg";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "./AppText";
import { Button } from "./Button";
import { Enter } from "./Enter";
import { Icon, type IconName } from "../icons/Icon";

export interface EmptyAction {
  label: string;
  icon?: IconName;
  onPress(): void;
  disabled?: boolean;
  loading?: boolean;
}

interface EmptyStateProps {
  title?: string;
  detail?: string | null;
  icon?: IconName;
  busy?: boolean;
  tone?: "default" | "danger";
  action?: EmptyAction;
  secondary?: EmptyAction;
  /** `inline` sits inside lists and sheets: no halo, smaller type. */
  size?: "hero" | "inline";
  /** Hero only: an illustration (Brain's seal cat) in place of the halo. */
  art?: React.ReactNode;
  style?: StyleProp<ViewStyle>;
}

const HALO = 88;

/** Shared empty, loading and error state for screens, lists and sheets. */
export function EmptyState({
  title,
  detail,
  icon,
  busy = false,
  tone = "default",
  action,
  secondary,
  size = "hero",
  art,
  style,
}: EmptyStateProps) {
  const { colors, theme } = useAppTheme();
  const danger = tone === "danger";
  const glyphColor = danger ? colors.dangerText : colors.textSecondary;

  if (size === "inline") {
    return (
      <View style={[styles.inline, style]} accessibilityLiveRegion="polite">
        {busy ? <ActivityIndicator color={colors.textSecondary} /> : null}
        {title ? (
          <AppText variant="label" tone={danger ? "danger" : "secondary"} style={styles.center}>
            {title}
          </AppText>
        ) : null}
        {detail ? (
          <AppText variant="caption" tone={danger ? "danger" : "tertiary"} style={styles.center}>
            {detail}
          </AppText>
        ) : null}
        {action ? (
          <Button size="sm" variant="tinted" {...actionProps(action)} style={styles.inlineAction} />
        ) : null}
      </View>
    );
  }

  return (
    <Enter preset="fade" style={[styles.hero, style]}>
      <View
        style={art ? styles.art : styles.halo}
        accessible={false}
        importantForAccessibility="no-hide-descendants"
      >
        {art ?? <Halo color={glyphColor} fill={danger ? colors.dangerSoft : theme.materials.tint} busy={busy} icon={icon} />}
      </View>
      {title ? (
        <AppText
          variant="title"
          accessibilityRole="header"
          accessibilityLiveRegion="polite"
          style={[styles.center, styles.title]}
        >
          {title}
        </AppText>
      ) : null}
      {detail ? (
        <AppText variant="body" tone={danger ? "danger" : "secondary"} style={[styles.center, styles.detail]}>
          {detail}
        </AppText>
      ) : null}
      {action || secondary ? (
        <View style={styles.actions}>
          {action ? <Button variant="filled" {...actionProps(action)} /> : null}
          {secondary ? <Button variant="plain" {...actionProps(secondary)} /> : null}
        </View>
      ) : null}
    </Enter>
  );
}

/** A quiet paper disc with a soft-ink glyph: no glow, no colour. */
function Halo({ color, fill, busy, icon }: { color: string; fill: string; busy: boolean; icon?: IconName }) {
  return (
    <>
      <Svg width={HALO} height={HALO} style={StyleSheet.absoluteFill}>
        <Circle cx={HALO / 2} cy={HALO / 2} r={HALO / 2 - 10} fill={fill} />
      </Svg>
      {busy ? (
        <ActivityIndicator color={color} />
      ) : icon ? (
        <Icon name={icon} size={28} color={color} />
      ) : null}
    </>
  );
}

function actionProps(action: EmptyAction) {
  return {
    label: action.label,
    icon: action.icon,
    onPress: action.onPress,
    disabled: action.disabled,
    loading: action.loading,
  };
}

const styles = StyleSheet.create({
  hero: {
    width: "100%",
    maxWidth: 420,
    alignSelf: "center",
    paddingHorizontal: 28,
    paddingVertical: 32,
    alignItems: "center",
  },
  halo: {
    width: HALO,
    height: HALO,
    alignItems: "center",
    justifyContent: "center",
    marginBottom: 18,
  },
  art: {
    marginBottom: 22,
  },
  title: {
    marginBottom: 6,
  },
  detail: {
    maxWidth: 320,
  },
  center: {
    textAlign: "center",
  },
  actions: {
    marginTop: 22,
    alignItems: "center",
    gap: 6,
  },
  inline: {
    minHeight: 120,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 18,
    gap: 6,
  },
  inlineAction: {
    marginTop: 8,
  },
});
