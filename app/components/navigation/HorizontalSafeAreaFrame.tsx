import React, { type ReactElement } from "react";
import { StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppColors } from "../../constants/tokens";
import { desktopScreenLayout } from "./DesktopWebShell";

/**
 * Keeps a screen's content out of the side insets: a foldable's rail, a
 * landscape notch. The frame wraps the content, never a scroll view, so lists
 * still scroll edge to edge inside it. Headers belong to the navigator.
 */
export function HorizontalSafeAreaFrame({ children }: { children: ReactElement | null }) {
  const colors = useAppColors();
  const { left, right } = useSafeAreaInsets();
  return (
    <View
      style={[
        styles.frame,
        { backgroundColor: colors.bgPrimary, paddingLeft: left, paddingRight: right },
      ]}
    >
      {children}
    </View>
  );
}

type ScreenLayoutProps = Parameters<typeof desktopScreenLayout>[0];

/** The root Stack's `screenLayout`: every page inside the side insets. */
export function appScreenLayout(props: ScreenLayoutProps) {
  return <HorizontalSafeAreaFrame>{desktopScreenLayout(props)}</HorizontalSafeAreaFrame>;
}

const styles = StyleSheet.create({
  frame: {
    flex: 1,
  },
});
