import type { ReactNode } from "react";
import { Animated, StyleSheet } from "react-native";
import TopTabs from "expo-router/js-top-tabs";
import { PrimaryDrawerShell } from "../../components/navigation/PrimaryDrawerShell";
import { useDesktopWeb } from "../../components/navigation/useDesktopWeb";
import { useCatLabs } from "../../store/catLabs";
import {
  PrimaryPagerPositionBridge,
  PrimaryPagerPositionProvider,
} from "../../components/navigation/primaryPagerPosition";

interface PrimaryTabsLayoutProps {
  children: ReactNode;
  navigation: {
    navigate(route: "index" | "list" | "cat"): void;
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
  const activeRoute = name === "list" ? "list" : name === "cat" ? "cat" : "brain";
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
  // The cat is a Labs feature: off, its page is not in the pager at all.
  const { enabled: catOn } = useCatLabs();
  return (
    <TopTabs
      backBehavior={desktopWeb ? "history" : "none"}
      layout={renderPrimaryTabsLayout}
      screenOptions={{
        animationEnabled: !desktopWeb,
        lazy: false,
        sceneStyle: styles.scene,
        swipeEnabled: !desktopWeb,
      }}
      tabBar={renderPrimaryTabBar}
    >
      <TopTabs.Screen name="index" options={{ title: "Brain" }} />
      <TopTabs.Screen name="list" options={{ title: "Sessions" }} />
      <TopTabs.Protected guard={catOn}>
        {/* The house loads its bundle and a WebView: only when first opened. */}
        <TopTabs.Screen name="cat" options={{ title: "Cat", lazy: true }} />
      </TopTabs.Protected>
    </TopTabs>
  );
}

const styles = StyleSheet.create({
  scene: {
    backgroundColor: "transparent",
    flex: 1,
  },
});
