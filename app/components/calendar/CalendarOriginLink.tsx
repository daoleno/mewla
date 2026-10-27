import React, { useCallback, useMemo } from "react";
import { Pressable, StyleSheet, Text, type StyleProp, type ViewStyle } from "react-native";
import { useRouter } from "expo-router";
import { TypeScale, useAppColors } from "../../constants/tokens";
import { Icon } from "../icons/Icon";
import { selectCurrentServerCalendar, useCalendar } from "../../store/calendar";
import {
  calendarOriginLabel,
  type CalendarOrigin,
} from "../../services/calendarRuns";

/**
 * Where Calendar-born Work came from, and the way back to it: the schedule's
 * name and the run's date ("运营：挖稿 · Oct 27"), opening that Calendar item
 * at that run. Null for Work Calendar did not start.
 */
export function useCalendarOrigin(
  serverId: string | null | undefined,
  origin: CalendarOrigin | null,
  fallback: { title?: string; at?: string } = {},
) {
  const router = useRouter();
  const state = useCalendar();
  const items = serverId ? selectCurrentServerCalendar(state, serverId)?.items : undefined;
  const label = useMemo(
    () => (origin ? calendarOriginLabel(origin, items ?? [], fallback) : null),
    [fallback.at, fallback.title, items, origin?.itemId, origin?.runId],
  );
  const open = useCallback(() => {
    if (!origin || !serverId) return;
    // navigate, not push: back on an open Calendar instead of stacking another.
    router.navigate({
      pathname: "/calendar",
      params: { id: origin.itemId, serverId, ...(origin.runId ? { run: origin.runId } : {}) },
    });
  }, [origin?.itemId, origin?.runId, router, serverId]);
  return useMemo(() => (label && serverId ? { ...label, open } : null), [label, open, serverId]);
}

export function CalendarOriginLink({
  label,
  onPress,
  style,
}: {
  label: string;
  onPress(): void;
  style?: StyleProp<ViewStyle>;
}) {
  const colors = useAppColors();
  return (
    <Pressable
      accessibilityRole="link"
      accessibilityLabel={`From Calendar: ${label}`}
      hitSlop={8}
      onPress={onPress}
      style={({ pressed }) => [styles.link, pressed ? { opacity: 0.6 } : null, style]}
    >
      <Icon name="calendar" size={14} color={colors.textSecondary} />
      <Text numberOfLines={1} style={[styles.text, { color: colors.textSecondary }]}>
        {label}
      </Text>
    </Pressable>
  );
}

/** The glyph and label without a tap, inside a row that already opens something. */
export function CalendarOriginMark({ label }: { label: string }) {
  const colors = useAppColors();
  return (
    <>
      <Icon name="calendar" size={13} color={colors.textTertiary} />
      <Text numberOfLines={1} style={[styles.text, { color: colors.textTertiary }]}>
        {label}
      </Text>
    </>
  );
}

const styles = StyleSheet.create({
  link: {
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    alignSelf: "flex-start",
    maxWidth: "100%",
    minHeight: 24,
  },
  text: { ...TypeScale.caption, flexShrink: 1 },
});
