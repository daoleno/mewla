import React from "react";
import { StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "./AppText";
import { StatusMark } from "./StatusMark";
import { workStatusTextInk, type WorkStatus } from "./workStatus";

/**
 * success: Ready check · warning: amber triangle · danger: Failed box ·
 * needs: the seal pill · neutral: a plain paper tag · accent: an ink tag, or
 * the spinning Running arc when `live`.
 */
export type StatusTone = "success" | "warning" | "danger" | "accent" | "needs" | "neutral";

interface StatusPillProps {
  label: string;
  tone?: StatusTone;
  /** Live states such as Connecting spin the Running arc. */
  live?: boolean;
  style?: StyleProp<ViewStyle>;
}

const TONE_STATUS: Record<Exclude<StatusTone, "neutral">, WorkStatus> = {
  success: "ready",
  warning: "warning",
  danger: "failed",
  accent: "running",
  needs: "needs",
};

/**
 * Seal & Slip status: a small glyph and a word in soft type. Only "Needs you"
 * is a filled pill, in the seal; everything else stays quiet on the paper.
 */
export function StatusPill({ label, tone = "neutral", live = false, style }: StatusPillProps) {
  const { colors, theme } = useAppTheme();
  if (tone === "needs") {
    return (
      <View style={[styles.sealPill, { backgroundColor: colors.seal }, style]} accessibilityLabel={label}>
        <AppText variant="micro" numberOfLines={1} style={{ color: colors.onSeal }}>
          {label}
        </AppText>
      </View>
    );
  }
  // Only live work spins; an informational accent ("Public", "From …") is
  // an ink tag, never a Running mark.
  if ((tone === "neutral" || tone === "accent") && !live) {
    return (
      <View style={[styles.tag, { backgroundColor: theme.materials.tint }, style]} accessibilityLabel={label}>
        <AppText
          variant="micro"
          numberOfLines={1}
          style={{ color: tone === "accent" ? colors.textPrimary : colors.textSecondary }}
        >
          {label}
        </AppText>
      </View>
    );
  }
  const status = live ? "running" : TONE_STATUS[tone as Exclude<StatusTone, "neutral">];
  const ink = workStatusTextInk(status, {
    statusReady: colors.statusDone,
    statusRunning: colors.statusRunning,
    seal: colors.seal,
    sealText: colors.sealText,
    statusWarning: colors.statusWarning,
    statusFailed: colors.statusFailed,
    statusBlocked: colors.statusBlocked,
  });
  return (
    <View style={[styles.mark, style]} accessibilityLabel={label}>
      <StatusMark status={status} />
      <AppText variant="micro" numberOfLines={1} style={{ color: ink }}>
        {label}
      </AppText>
    </View>
  );
}

const styles = StyleSheet.create({
  mark: {
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    minHeight: 22,
    alignSelf: "flex-start",
  },
  tag: {
    minHeight: 22,
    paddingHorizontal: 8,
    borderRadius: 999,
    alignSelf: "flex-start",
    justifyContent: "center",
  },
  sealPill: {
    minHeight: 22,
    paddingHorizontal: 9,
    borderRadius: 999,
    alignSelf: "flex-start",
    justifyContent: "center",
  },
});
