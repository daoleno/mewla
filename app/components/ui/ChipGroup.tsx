import React from "react";
import { Pressable, StyleSheet, View } from "react-native";
import * as Haptics from "expo-haptics";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "./AppText";
import { Icon, type IconName } from "../icons/Icon";

export interface ChipOption<T extends string> {
  value: T;
  label: string;
  icon?: IconName;
  /** Custom glyph such as a brand mark; replaces `icon`. */
  leading?: React.ReactNode;
  /** A trailing tally, such as a process count, in the muted ink beside the label. */
  count?: number;
  /** Replaces the spoken label when the visible one needs context. */
  accessibilityLabel?: string;
  /** Draws the ink in the warning colour, for a value that is news. */
  warn?: boolean;
}

type ChipGroupProps<T extends string> = {
  options: readonly ChipOption<T>[];
  accessibilityLabel: string;
} & (
  | { multiple?: false; value: T | null; onChange(value: T): void }
  | { multiple: true; value: readonly T[]; onChange(value: T[]): void }
);

/**
 * Wrapping pill chips for a filter that has too many, or too variable, options
 * for SegmentedControl: one exclusive choice, or any number of toggles with
 * `multiple`. A toggle also draws a check, so the state is not carried by colour alone.
 */
export function ChipGroup<T extends string>(props: ChipGroupProps<T>) {
  const { options, accessibilityLabel } = props;
  const { colors } = useAppTheme();
  const selection: readonly T[] = props.multiple ? props.value : props.value === null ? [] : [props.value];
  return (
    <View
      accessibilityRole={props.multiple ? undefined : "radiogroup"}
      accessibilityLabel={accessibilityLabel}
      style={styles.wrap}
    >
      {options.map((option) => {
        const selected = selection.includes(option.value);
        const ink = option.warn
          ? colors.warning
          : selected
            ? colors.textPrimary
            : colors.textSecondary;
        return (
          <Pressable
            key={option.value}
            accessibilityRole={props.multiple ? "checkbox" : "radio"}
            accessibilityLabel={option.accessibilityLabel ?? option.label}
            accessibilityState={{ checked: selected, selected }}
            onPress={() => {
              void Haptics.selectionAsync();
              if (props.multiple) {
                props.onChange(
                  selected
                    ? props.value.filter((item) => item !== option.value)
                    : [...props.value, option.value],
                );
              } else if (!selected) {
                props.onChange(option.value);
              }
            }}
            style={({ pressed }) => [
              styles.chip,
              {
                backgroundColor: selected ? colors.surfaceSubtle : "transparent",
                borderColor: selected ? colors.borderStrong : colors.border,
              },
              pressed && !selected ? { opacity: 0.6 } : null,
            ]}
          >
            {option.leading ?? (option.icon ? <Icon name={option.icon} size={14} color={ink} /> : null)}
            <AppText variant="label" numberOfLines={1} style={{ color: ink }}>
              {option.label}
            </AppText>
            {option.count !== undefined ? (
              <AppText variant="caption" numberOfLines={1} style={[styles.count, { color: ink }]}>
                {option.count}
              </AppText>
            ) : null}
            {props.multiple && selected ? <Icon name="check" size={14} color={ink} /> : null}
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
  chip: {
    minHeight: 40,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 999,
  },
  count: { fontVariant: ["tabular-nums"] },
});
