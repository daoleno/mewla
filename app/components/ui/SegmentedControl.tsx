import React, { useState } from "react";
import {
  Animated,
  Pressable,
  StyleSheet,
  View,
  type LayoutChangeEvent,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import * as Haptics from "expo-haptics";
import { TouchTarget, shadow, useAppTheme } from "../../constants/tokens";
import { AppText } from "./AppText";
import { Icon, type IconName } from "../icons/Icon";

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  icon?: IconName;
  /** Custom glyph such as a brand mark; replaces `icon`. */
  leading?: React.ReactNode;
}

interface SegmentedControlProps<T extends string> {
  options: readonly SegmentedOption<T>[];
  value: T;
  onChange(value: T): void;
  accessibilityLabel: string;
  /**
   * A pager's position (0 … options.length - 1). The thumb and the active
   * ink then follow the finger through a swipe instead of jumping on
   * release; see SegmentedPager.
   */
  position?: Animated.AnimatedInterpolation<number>;
  style?: StyleProp<ViewStyle>;
}

const TRACK_PADDING = 3;
const SEGMENT_GAP = 2;

/**
 * Inset segmented control for a small exclusive choice, such as appearance.
 * The selected segment is a raised thumb on a sunken track.
 */
export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  accessibilityLabel,
  position,
  style,
}: SegmentedControlProps<T>) {
  const { colors, theme } = useAppTheme();
  const [trackWidth, setTrackWidth] = useState(0);
  const thumbStyle = {
    // Paper on the tinted track; the hairline keeps the thumb visible where
    // the shadow does not draw (web, Android).
    backgroundColor: theme.isLight ? colors.bgSurface : colors.surfaceActive,
    borderColor: theme.isLight ? colors.border : "transparent",
    ...shadow("card", colors.shadowColor),
  };
  const segmentWidth = Math.max(
    0,
    (trackWidth - TRACK_PADDING * 2 - SEGMENT_GAP * (options.length - 1)) / options.length,
  );
  const thumbX =
    position && options.length > 1
      ? position.interpolate({
          inputRange: options.map((_, index) => index),
          outputRange: options.map((_, index) => index * (segmentWidth + SEGMENT_GAP)),
          extrapolate: "clamp",
        })
      : null;
  // Until the track is measured the selected segment draws its own thumb.
  const thumbFollows = thumbX !== null && segmentWidth > 0;
  return (
    <View
      accessibilityRole="radiogroup"
      accessibilityLabel={accessibilityLabel}
      onLayout={position ? (event: LayoutChangeEvent) => setTrackWidth(event.nativeEvent.layout.width) : undefined}
      style={[styles.track, { backgroundColor: colors.surfaceSubtle }, style]}
    >
      {thumbFollows ? (
        <Animated.View
          pointerEvents="none"
          style={[styles.thumb, thumbStyle, { width: segmentWidth, transform: [{ translateX: thumbX }] }]}
        />
      ) : null}
      {options.map((option, index) => {
        const selected = option.value === value;
        const content = (ink: string) => (
          <>
            {option.leading ?? (option.icon ? <Icon name={option.icon} size={16} color={ink} /> : null)}
            <AppText variant="label" numberOfLines={1} style={{ color: ink }}>
              {option.label}
            </AppText>
          </>
        );
        return (
          <Pressable
            key={option.value}
            accessibilityRole="radio"
            accessibilityLabel={option.label}
            accessibilityState={{ checked: selected, selected }}
            onPress={() => {
              if (selected) return;
              void Haptics.selectionAsync();
              onChange(option.value);
            }}
            style={({ pressed }) => [
              styles.segment,
              selected && !thumbFollows ? [styles.selected, thumbStyle] : null,
              pressed && !selected ? { opacity: 0.6 } : null,
            ]}
          >
            {position ? (
              <>
                {content(colors.textSecondary)}
                <Animated.View
                  aria-hidden
                  pointerEvents="none"
                  style={[
                    styles.activeInk,
                    {
                      opacity: position.interpolate({
                        inputRange: [index - 1, index, index + 1],
                        outputRange: [0, 1, 0],
                        extrapolate: "clamp",
                      }),
                    },
                  ]}
                >
                  {content(colors.textPrimary)}
                </Animated.View>
              </>
            ) : (
              content(selected ? colors.textPrimary : colors.textSecondary)
            )}
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  track: {
    flexDirection: "row",
    padding: TRACK_PADDING,
    borderRadius: 999,
    gap: SEGMENT_GAP,
  },
  segment: {
    flex: 1,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "transparent",
    minHeight: TouchTarget - 6,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
    paddingHorizontal: 8,
    borderRadius: 999,
  },
  selected: {},
  thumb: {
    position: "absolute",
    left: TRACK_PADDING,
    top: TRACK_PADDING,
    bottom: TRACK_PADDING,
    borderRadius: 999,
    borderWidth: StyleSheet.hairlineWidth,
  },
  // Same row as the segment, drawn over the secondary ink.
  activeInk: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
    paddingHorizontal: 8,
  },
});
