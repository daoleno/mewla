import type { ReactNode } from "react";
import { Animated, StyleSheet, type ViewStyle } from "react-native";
import TopTabs from "expo-router/js-top-tabs";
import { PrimaryDrawerShell } from "../../components/navigation/PrimaryDrawerShell";
import { useDesktopWeb } from "../../components/navigation/useDesktopWeb";
import {
  PrimaryPagerPositionBridge,
  PrimaryPagerPositionProvider,
} from "../../components/navigation/primaryPagerPosition";

interface PrimaryTabsLayoutProps {
  children: ReactNode;
  navigation: {
    navigate(route: "index" | "list"): void;
  };
  state: {
    index: number;
    routes: Array<{ name: string }>;
  };
}

interface PrimaryTabBarProps {
  position: Animated.AnimatedInterpolation<number>;
}

const renderPrimaryTabsLayout = ({
  children,
  navigation,
  state,
}: PrimaryTabsLayoutProps) => {
  const name = state.routes[state.index]?.name;
  const activeRoute = name === "list" ? "list" : "brain";
  return (
    <PrimaryPagerPositionProvider>
      <PrimaryDrawerShell
        activePrimaryRoute={activeRoute}
        onSelectPrimaryRoute={(target) => {
          navigation.navigate(target === "brain" ? "index" : target);
        }}
      >
        {children}
      </PrimaryDrawerShell>
    </PrimaryPagerPositionProvider>
  );
};

function renderPrimaryTabBar({ position }: PrimaryTabBarProps) {
  return <PrimaryPagerPositionBridge position={position} />;
}

export default function PrimaryLayout() {
  // Desktop web: Brain and Sessions are pages with history, not a pager. No
  // swipe either, so dragging across a chat selects its text.
  const desktopWeb = useDesktopWeb();
  return (
    <TopTabs
      backBehavior={desktopWeb ? "history" : "none"}
      layout={renderPrimaryTabsLayout}
      screenOptions={({ navigation }: { navigation: { isFocused(): boolean } }) => ({
        animationEnabled: !desktopWeb,
        lazy: false,
        // The other page stays mounted beside this one; on desktop hide it so
        // Tab and screen readers never land in a page that isn't showing.
        sceneStyle:
          desktopWeb && !navigation.isFocused()
            ? [styles.scene, styles.offscreen]
            : styles.scene,
        swipeEnabled: !desktopWeb,
      })}
      tabBar={renderPrimaryTabBar}
    >
      <TopTabs.Screen name="index" options={{ title: "Brain" }} />
      <TopTabs.Screen name="list" options={{ title: "Sessions" }} />
    </TopTabs>
  );
}

const styles = StyleSheet.create({
  scene: {
    backgroundColor: "transparent",
    flex: 1,
  },
  // Web-only CSS: keeps layout and scroll, drops focus and accessibility.
  offscreen: { visibility: "hidden" } as ViewStyle,
});
