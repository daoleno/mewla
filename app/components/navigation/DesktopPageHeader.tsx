import React, { type ReactNode } from "react";
import { StyleSheet, Text, View } from "react-native";
import {
  DESKTOP_HEADER_HEIGHT,
  DESKTOP_PAGE_GUTTER,
  Spacing,
  TypeScale,
  useAppColors,
} from "../../constants/tokens";

interface DesktopPageHeaderProps {
  title: ReactNode;
  /** Back, on sub-pages. */
  leading?: ReactNode;
  /** The page's own actions. */
  trailing?: ReactNode;
}

/**
 * The one desktop web page header. It spans the whole pane: the title sits at
 * the page gutter and the actions at the opposite gutter, whatever width the
 * page's content column holds below it, so switching pages never moves either.
 */
export function DesktopPageHeader({ title, leading, trailing }: DesktopPageHeaderProps) {
  const colors = useAppColors();
  return (
    <View style={[styles.bar, { backgroundColor: colors.bgPrimary }]}>
      <View style={styles.row}>
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
