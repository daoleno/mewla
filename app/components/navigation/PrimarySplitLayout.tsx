import React, { type ReactNode } from "react";
import { StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";

interface PrimarySplitLayoutProps {
  /** A sidebar beside the page, or null when the page has the whole window. */
  sidebar: ReactNode;
  style?: StyleProp<ViewStyle>;
  children: ReactNode;
}

/**
 * The primary surface with an optional sidebar beside it. The sidebar is a
 * sibling in a fixed slot, and the page always renders under the same
 * parents at the same position: folding or unfolding a device adds or removes
 * the sidebar without remounting the page, so its state survives.
 */
export function PrimarySplitLayout({ sidebar, style, children }: PrimarySplitLayoutProps) {
  return (
    <View style={[styles.root, style]}>
      {sidebar}
      <View style={styles.page}>{children}</View>
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    flexDirection: "row",
    overflow: "hidden",
  },
  page: {
    flex: 1,
    minWidth: 0,
  },
});
