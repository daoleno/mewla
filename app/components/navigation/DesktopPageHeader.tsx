import React, { type ReactNode } from "react";
import { StyleSheet, Text, View } from "react-native";
import type { DesktopPageColumn } from "./desktopWeb";
import {
  ContentWidth,
  DESKTOP_HEADER_HEIGHT,
  DESKTOP_PAGE_GUTTER,
  Spacing,
  TypeScale,
  useAppColors,
} from "../../constants/tokens";

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
 * The one desktop web page header. Its row is the page's own content column,
 * centered: the title sits where the content starts and the actions end where
 * it ends, so a title is never detached from what it names. Pages of
 * different widths may move it a little; the sidebar never moves.
 */
export function DesktopPageHeader({ title, column, leading, trailing }: DesktopPageHeaderProps) {
  const colors = useAppColors();
  return (
    <View style={[styles.bar, { backgroundColor: colors.bgPrimary }]}>
      <View style={[styles.row, column ? { maxWidth: ContentWidth[column], paddingHorizontal: Spacing.lg } : styles.fullRow]}>
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
    // The bar is a row: auto side margins center the column in it.
    marginHorizontal: "auto",
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
