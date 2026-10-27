import React, { type ReactNode } from "react";
import { StyleSheet, Text, View } from "react-native";
import {
  ContentWidth,
  DESKTOP_HEADER_HEIGHT,
  Spacing,
  TypeScale,
  useAppColors,
} from "../../constants/tokens";
import type { DesktopPageColumn } from "./desktopWeb";

interface DesktopPageHeaderProps {
  title: ReactNode;
  /** The page's content column; null lays the header across the full width. */
  column: DesktopPageColumn | null;
  /** Back, on sub-pages. */
  leading?: ReactNode;
  /** The page's own actions. */
  trailing?: ReactNode;
}

/**
 * The one desktop web page header. Its title sits on the content column's
 * left edge, so a page reads as one column instead of a title floating at the
 * window edge over centered content.
 */
export function DesktopPageHeader({ title, column, leading, trailing }: DesktopPageHeaderProps) {
  const colors = useAppColors();
  return (
    <View style={[styles.bar, { backgroundColor: colors.bgPrimary }]}>
      <View
        style={[
          styles.row,
          column ? { maxWidth: ContentWidth[column], paddingHorizontal: Spacing.lg } : styles.fullRow,
        ]}
      >
        {leading}
        {typeof title === "string" ? (
          <Text
            accessibilityRole="header"
            numberOfLines={1}
            style={[styles.title, { color: colors.textPrimary }]}
          >
            {title}
          </Text>
        ) : (
          <View style={styles.titleSlot}>{title}</View>
        )}
        {trailing ? <View style={styles.trailing}>{trailing}</View> : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    height: DESKTOP_HEADER_HEIGHT,
    flexDirection: "row",
    justifyContent: "center",
  },
  row: {
    flex: 1,
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.sm,
  },
  fullRow: {
    paddingHorizontal: Spacing.xl,
  },
  title: {
    ...TypeScale.pageTitle,
    flex: 1,
    minWidth: 0,
  },
  titleSlot: {
    flex: 1,
    minWidth: 0,
  },
  trailing: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.xs,
  },
});
