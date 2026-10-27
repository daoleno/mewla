import React, { useCallback, useMemo, type ReactNode } from "react";
import { StyleSheet, View, useWindowDimensions, type StyleProp, type ViewStyle } from "react-native";
import { TabView } from "react-native-tab-view";
import { useReducedMotion } from "react-native-reanimated";
import { SegmentedControl, type SegmentedOption } from "./SegmentedControl";

interface SegmentedPagerProps<T extends string> {
  accessibilityLabel: string;
  options: readonly SegmentedOption<T>[];
  value: T;
  onChange(value: T): void;
  renderPage(value: T): ReactNode;
  /** Drawn above the switch, in the same header. */
  header?: ReactNode;
  headerStyle?: StyleProp<ViewStyle>;
}

/**
 * Peer pages behind a segmented switch (Plugins · Skills): a native pager
 * (react-native-tab-view on react-native-pager-view, as Brain · Sessions and
 * Stats use), so the pages swipe and the thumb follows the finger. Every page
 * stays mounted, so a swipe shows the next page instead of a blank one.
 * Horizontal scrollers inside a page keep their own drags.
 */
export function SegmentedPager<T extends string>({
  accessibilityLabel,
  options,
  value,
  onChange,
  renderPage,
  header,
  headerStyle,
}: SegmentedPagerProps<T>) {
  const { width } = useWindowDimensions();
  const reducedMotion = useReducedMotion();
  const routes = useMemo(
    () => options.map((option) => ({ key: option.value, title: option.label })),
    [options],
  );
  const index = Math.max(0, options.findIndex((option) => option.value === value));
  const onIndexChange = useCallback(
    (next: number) => {
      const option = options[next];
      if (option && option.value !== value) onChange(option.value);
    },
    [onChange, options, value],
  );
  return (
    <TabView<{ key: T; title: string }>
      navigationState={{ index, routes }}
      initialLayout={{ width }}
      animationEnabled={!reducedMotion}
      lazy={false}
      overScrollMode="never"
      onIndexChange={onIndexChange}
      renderTabBar={({ position, jumpTo }) => (
        <View style={headerStyle}>
          {header}
          <SegmentedControl
            accessibilityLabel={accessibilityLabel}
            options={options}
            value={value}
            position={position}
            onChange={jumpTo}
          />
        </View>
      )}
      renderScene={({ route }) => (
        // Off-screen pages stay mounted but out of screen-reader order.
        <View
          accessibilityElementsHidden={route.key !== value}
          importantForAccessibility={route.key === value ? "auto" : "no-hide-descendants"}
          style={styles.page}
        >
          {renderPage(route.key)}
        </View>
      )}
      style={styles.pager}
    />
  );
}

const styles = StyleSheet.create({
  pager: { flex: 1 },
  page: { flex: 1 },
});
