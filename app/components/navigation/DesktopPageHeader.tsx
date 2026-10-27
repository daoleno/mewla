import React, { type ReactNode } from "react";
import { StyleSheet, Text, View } from "react-native";
import {
  ContentWidth,
  DESKTOP_HEADER_HEIGHT,
  DESKTOP_PAGE_GUTTER,
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
 * The one desktop web page header. Its title sits at the page gutter, where
 * every page's content starts, so switching pages never moves it; the
 * actions end where the content column ends.
 */
export function DesktopPageHeader({ title, column, leading, trailing }: DesktopPageHeaderProps) {
  const colors = useAppColors();
  return (
    <View style={[styles.bar, { backgroundColor: colors.bgPrimary }]}>
      <View
        style={[
          styles.row,
          column
            ? { maxWidth: ContentWidth[column], marginLeft: DESKTOP_PAGE_GUTTER - Spacing.lg, paddingHorizontal: Spacing.lg }
            : styles.fullRow,
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
  },
  row: {
    flex: 1,
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.sm,
  },
  fullRow: {
    paddingHorizontal: DESKTOP_PAGE_GUTTER,
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
