import type { ReactElement } from "react";
import { StyleSheet, View, type ViewStyle } from "react-native";
import { useIsFocused } from "expo-router";
import { useDesktopWeb } from "./useDesktopWeb";

/** Keep the hidden desktop tab out of keyboard navigation and screen readers.
 * Focus must come from the render-time context: navigation.isFocused() in
 * screenOptions can still describe the previous tab during a navigation.
 */
export function PrimaryTabScene({ children }: { children: ReactElement }) {
  const focused = useIsFocused();
  const desktopWeb = useDesktopWeb();
  return (
    <View style={[styles.scene, desktopWeb && !focused ? styles.offscreen : null]}>
      {children}
    </View>
  );
}

export function primaryTabScreenLayout({ children }: { children: ReactElement }) {
  return <PrimaryTabScene>{children}</PrimaryTabScene>;
}

const styles = StyleSheet.create({
  scene: { flex: 1 },
  // Preserve the pager's width and mounted state, but exclude its controls.
  offscreen: { visibility: "hidden" } as ViewStyle,
});
