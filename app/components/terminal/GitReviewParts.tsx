import React from "react";
import { ActivityIndicator, Pressable, StyleSheet, Text, View, type StyleProp, type ViewStyle } from "react-native";
import * as Haptics from "expo-haptics";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import { ContinuousCorners, Radii, TouchTarget, TypeScale, UiTextMetrics } from "../../constants/tokens";
import type { GitDiffFileInfo } from "../../services/gitDiff";
import { Icon, type IconName } from "../icons/Icon";
import { withAlpha } from "./colorWithAlpha";
import { gitDiffStatusMark, gitDiffToneColor, gitDiffTone } from "./gitDiffPresentation";

/** The file's status as one small mark: no tile, no column of letters. */
export function GitStatusMark({
  file,
  theme,
  chrome,
}: {
  file: Pick<GitDiffFileInfo, "status" | "binary">;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
}) {
  const tone = gitDiffTone(file);
  const color = gitDiffToneColor(tone, theme, chrome.textSubtle, chrome.textMuted);
  return (
    <Text accessible={false} style={[styles.mark, { color }]}>
      {gitDiffStatusMark(tone)}
    </Text>
  );
}

/** `+12 −3`, in the terminal's change colours; a binary file says so. */
export function GitDelta({
  additions,
  deletions,
  binary,
  theme,
  chrome,
  style,
}: {
  additions: number;
  deletions: number;
  binary?: boolean;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  style?: StyleProp<ViewStyle>;
}) {
  if (binary) {
    return <Text style={[styles.delta, { color: chrome.textSubtle }]}>bin</Text>;
  }
  return (
    <View style={[styles.deltaRow, style]}>
      {additions > 0 ? <Text style={[styles.delta, { color: theme.green }]}>{`+${additions}`}</Text> : null}
      {deletions > 0 ? <Text style={[styles.delta, { color: theme.red }]}>{`−${deletions}`}</Text> : null}
    </View>
  );
}

/** A quiet icon control with a full touch target around a small glyph. */
export function GitIconButton({
  icon,
  label,
  chrome,
  onPress,
  disabled,
  busy,
  selected,
  size = TouchTarget,
  color,
}: {
  icon: IconName;
  label: string;
  chrome: TerminalThemeChrome;
  onPress(): void;
  disabled?: boolean;
  busy?: boolean;
  selected?: boolean;
  size?: number;
  color?: string;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled: Boolean(disabled), busy: Boolean(busy), selected: Boolean(selected) }}
      disabled={disabled || busy}
      onPress={() => {
        void Haptics.selectionAsync();
        onPress();
      }}
      style={({ pressed }) => [
        styles.iconButton,
        { width: size, height: size, opacity: disabled ? 0.38 : pressed ? 0.55 : 1 },
        selected ? { backgroundColor: withAlpha(chrome.text, 0.08) } : null,
      ]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={chrome.textMuted} />
      ) : (
        <Icon name={icon} size={20} color={color ?? (selected ? chrome.text : chrome.textMuted)} />
      )}
    </Pressable>
  );
}

/** A compact text button: `Stage all`, `Push ↑2`. Tinted when it is the next step. */
export function GitTextButton({
  label,
  chrome,
  onPress,
  tone = "plain",
  busy,
  disabled,
  icon,
  accessibilityLabel,
  style,
}: {
  label: string;
  chrome: TerminalThemeChrome;
  onPress(): void;
  tone?: "plain" | "tinted" | "primary" | "danger";
  busy?: boolean;
  disabled?: boolean;
  icon?: IconName;
  accessibilityLabel?: string;
  style?: StyleProp<ViewStyle>;
}) {
  const ink =
    tone === "primary" ? chrome.textOnAccent : tone === "danger" ? chrome.danger : tone === "tinted" ? chrome.text : chrome.link;
  const background =
    tone === "primary" ? chrome.accent : tone === "tinted" ? withAlpha(chrome.text, 0.08) : tone === "danger" ? chrome.dangerSoft : "transparent";
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel ?? label}
      accessibilityState={{ disabled: Boolean(disabled), busy: Boolean(busy) }}
      disabled={disabled || busy}
      hitSlop={{ top: 6, bottom: 6 }}
      onPress={onPress}
      style={({ pressed }) => [
        styles.textButton,
        { backgroundColor: background, opacity: disabled ? 0.38 : pressed ? 0.6 : 1 },
        style,
      ]}
    >
      {busy ? <ActivityIndicator size="small" color={ink} /> : icon ? <Icon name={icon} size={15} color={ink} /> : null}
      <Text numberOfLines={1} style={[styles.textButtonLabel, { color: ink }]}>
        {label}
      </Text>
    </Pressable>
  );
}

/** Two text tabs with an ink underline: the same switch as Brain and Sessions. */
export function GitTabs<T extends string>({
  tabs,
  value,
  chrome,
  onChange,
}: {
  tabs: readonly { value: T; label: string }[];
  value: T;
  chrome: TerminalThemeChrome;
  onChange(value: T): void;
}) {
  return (
    <View accessibilityRole="tablist" style={styles.tabs}>
      {tabs.map((tab) => {
        const selected = tab.value === value;
        return (
          <Pressable
            key={tab.value}
            accessibilityRole="tab"
            accessibilityState={{ selected }}
            hitSlop={{ top: 8, bottom: 8 }}
            onPress={() => {
              if (!selected) void Haptics.selectionAsync();
              onChange(tab.value);
            }}
            style={styles.tab}
          >
            <Text style={[styles.tabLabel, { color: selected ? chrome.text : chrome.textSubtle, fontWeight: selected ? "600" : "500" }]}>
              {tab.label}
            </Text>
            <View style={[styles.underline, { backgroundColor: selected ? chrome.text : "transparent" }]} />
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  mark: { width: 16, textAlign: "center", ...TypeScale.label, fontWeight: "700", ...UiTextMetrics },
  deltaRow: { flexDirection: "row", gap: 6 },
  delta: { ...TypeScale.caption, ...UiTextMetrics, fontVariant: ["tabular-nums"] },
  iconButton: { alignItems: "center", justifyContent: "center", borderRadius: Radii.sm, ...ContinuousCorners },
  textButton: {
    minHeight: 32,
    paddingHorizontal: 12,
    borderRadius: 16,
    ...ContinuousCorners,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  textButtonLabel: { ...TypeScale.label, ...UiTextMetrics },
  tabs: { flexDirection: "row", alignItems: "center", gap: 18 },
  tab: { height: 32, alignItems: "center", justifyContent: "center" },
  tabLabel: { ...TypeScale.heading, fontSize: 16, ...UiTextMetrics },
  underline: { position: "absolute", bottom: 0, left: 0, right: 0, height: 2, borderRadius: 1 },
});
