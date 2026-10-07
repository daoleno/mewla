import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import {
  Keyboard,
  Platform,
  StyleSheet,
  Text,
  View,
  useWindowDimensions,
  type View as ViewInstance,
} from "react-native";
import { useIsFocused } from "expo-router";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Typography, useAppColors } from "../../constants/tokens";
import type { PrimaryRouteName } from "../../services/interactionTrace";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkerServerSummary } from "../../store/workers";
import { NavMenuIcon } from "./PrimaryNavIcons";
import { PrimaryChromeButton } from "./PrimaryChromeButton";
import {
  PrimaryAppBarPageAction,
  PrimaryPageActionProvider,
} from "./PrimaryPageAction";
import { PrimaryDrawer, usePrimaryDrawerController } from "./PrimaryDrawer";
import { PrimaryDrawerPanel } from "./PrimaryDrawerPanel";
import {
  PrimarySurfaceInteractionProvider,
} from "./PrimarySurfaceInteraction";
import { PrimaryTopSwitch } from "./PrimaryTopSwitch";
import {
  PrimarySelectionBarProvider,
  usePrimarySelectionBarContent,
} from "./PrimarySelectionBar";
import { resolvePrimaryAppBarGeometry } from "./primaryAppBarGeometry";
import { useDrawerFocusContainment } from "./useDrawerFocusContainment";
import { usePrimaryDrawerBack } from "./usePrimaryDrawerBack";

export {
  PRIMARY_APP_BAR_HEIGHT,
  PRIMARY_APP_BAR_LAYOUT_MODE,
  resolvePrimaryAppBarGeometry,
} from "./primaryAppBarGeometry";

interface PrimaryDrawerShellProps {
  activePrimaryRoute: PrimaryRouteName;
  children: ReactNode;
  onSelectPrimaryRoute(route: PrimaryRouteName): void;
}

interface PrimaryAppBarProps {
  activePrimaryRoute: PrimaryRouteName;
  /** The sidebar holds the menu and the Brain · Sessions switch. */
  docked: boolean;
  drawerVisible: boolean;
  menuButtonRef: RefObject<ViewInstance | null>;
  onOpenDrawer(): void;
  onOpenPressIn(): void;
  onSelectPrimaryRoute(route: PrimaryRouteName): void;
  topInset: number;
}

const PRIMARY_DRAWER_SWIPE_EDGE_WIDTH = 40;
/** From this width the drawer docks as a permanent sidebar (tablet, desktop). */
export const PRIMARY_SIDEBAR_BREAKPOINT = 1024;
const PRIMARY_SIDEBAR_WIDTH = 264;

function PrimaryAppBar({
  activePrimaryRoute,
  docked,
  drawerVisible,
  menuButtonRef,
  onOpenDrawer,
  onOpenPressIn,
  onSelectPrimaryRoute,
  topInset,
}: PrimaryAppBarProps) {
  const colors = useAppColors();
  const geometry = resolvePrimaryAppBarGeometry(topInset);
  const showBrainCanvas = activePrimaryRoute === "brain";
  const { currentServer } = useCurrentServer();
  const { serverConnections, serverConnectionIssues } = useWorkerServerSummary();
  const connection = currentServer
    ? serverConnections[currentServer.id] || "offline"
    : "offline";
  const connectionIssue = currentServer
    ? serverConnectionIssues[currentServer.id] ?? null
    : null;
  // The menu glyph stays clean while the current server is healthy. A dot
  // appears only when the user can act on it: oxblood for a connection issue,
  // amber while the server is offline. Connecting is transient and silent.
  const connectionBadge = !currentServer
    ? null
    : connectionIssue
      ? colors.statusFailed
      : connection === "offline"
        ? colors.warning
        : null;
  const menuLabel = !currentServer
    ? "Open navigation drawer, no server"
    : connectionIssue
      ? `Open navigation drawer, ${connectionIssue.title}`
      : connection === "offline"
        ? "Open navigation drawer, server offline"
        : "Open navigation drawer";
  const selectionBar = usePrimarySelectionBarContent();
  if (selectionBar != null) {
    return (
      <View
        style={[
          styles.appBar,
          styles.appBarOverlay,
          {
            paddingTop: geometry.safeAreaTop,
            minHeight: geometry.contentInset,
            backgroundColor: colors.bgPrimary,
            borderBottomColor: colors.borderSubtle,
          },
        ]}
      >
        <View style={styles.selectionBarSlot}>{selectionBar}</View>
      </View>
    );
  }
  return (
    <View
      style={[
        styles.appBar,
        styles.appBarOverlay,
        {
          paddingTop: geometry.safeAreaTop,
          minHeight: geometry.contentInset,
          backgroundColor: showBrainCanvas ? "transparent" : colors.bgPrimary,
          borderBottomColor: "transparent",
        },
      ]}
    >
      {docked ? (
        <Text
          accessibilityRole="header"
          numberOfLines={1}
          style={[styles.dockedTitle, { color: colors.textPrimary }]}
        >
          {activePrimaryRoute === "brain" ? "Brain" : "Sessions"}
        </Text>
      ) : (
      <>
      <PrimaryChromeButton
        ref={menuButtonRef}
        onPress={onOpenDrawer}
        onPressIn={onOpenPressIn}
        accessibilityRole="button"
        accessibilityLabel={menuLabel}
        tabIndex={drawerVisible ? -1 : 0}
        badgeColor={connectionBadge}
      >
        <NavMenuIcon color={colors.textPrimary} size={20} />
      </PrimaryChromeButton>
      <PrimaryTopSwitch
        activeRoute={activePrimaryRoute}
        onSelectRoute={onSelectPrimaryRoute}
      />
      </>
      )}
      <PrimaryAppBarPageAction drawerVisible={drawerVisible} />
    </View>
  );
}

export function PrimaryDrawerShell({
  activePrimaryRoute,
  children,
  onSelectPrimaryRoute,
}: PrimaryDrawerShellProps) {
  const colors = useAppColors();
  const { width: windowWidth } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const routeFocused = useIsFocused();
  const drawerWidth = Math.min(320, Math.max(240, windowWidth - 52));
  const docked = windowWidth >= PRIMARY_SIDEBAR_BREAKPOINT;
  const [restoreMenuFocus, setRestoreMenuFocus] = useState(false);
  const navigatingAwayRef = useRef(false);
  const primaryRef = useRef<ViewInstance>(null);
  const drawerRef = useRef<ViewInstance>(null);
  const menuButtonRef = useRef<ViewInstance>(null);
  const closeButtonRef = useRef<ViewInstance>(null);

  // React mirrors the UI-thread drawer target. Focus returns to the menu
  // button after any close except one that navigates away.
  const handleDrawerChange = useCallback((open: boolean) => {
    if (open) {
      navigatingAwayRef.current = false;
      setRestoreMenuFocus(false);
      return;
    }
    setRestoreMenuFocus(!navigatingAwayRef.current);
    navigatingAwayRef.current = false;
  }, []);
  const drawer = usePrimaryDrawerController(handleDrawerChange);
  const drawerOpen = drawer.open;
  const requestDrawer = drawer.request;

  const beginOpenInteraction = useCallback(() => {
    setRestoreMenuFocus(false);
  }, []);
  const openDrawer = useCallback(() => {
    setRestoreMenuFocus(false);
    Keyboard.dismiss();
    requestDrawer(true);
  }, [requestDrawer]);
  const closeDrawer = useCallback(() => {
    requestDrawer(false);
  }, [requestDrawer]);
  const dismissDrawerForNavigation = useCallback(() => {
    navigatingAwayRef.current = true;
    setRestoreMenuFocus(false);
    requestDrawer(false);
  }, [requestDrawer]);
  const consumeMenuFocusReturn = useCallback(() => {
    setRestoreMenuFocus(false);
  }, []);

  useEffect(() => {
    if (!routeFocused && drawerOpen) {
      dismissDrawerForNavigation();
    }
  }, [dismissDrawerForNavigation, drawerOpen, routeFocused]);

  useEffect(() => {
    if (Platform.OS !== "web" || !drawerOpen) {
      return;
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      closeDrawer();
    };
    document.addEventListener("keydown", handleKeyDown, true);
    return () => document.removeEventListener("keydown", handleKeyDown, true);
  }, [closeDrawer, drawerOpen]);

  useDrawerFocusContainment({
    closeButtonRef,
    drawerRef,
    drawerVisible: drawerOpen,
    menuButtonRef,
    onMenuFocusRestored: consumeMenuFocusReturn,
    primaryRef,
    restoreMenuFocus,
    routeFocused,
  });
  usePrimaryDrawerBack({
    enabled: routeFocused && drawerOpen,
    onBack: closeDrawer,
  });

  const primaryWebProps = useMemo(
    () => (Platform.OS === "web" ? { inert: drawerOpen } : {}),
    [drawerOpen],
  );
  const drawerWebProps = useMemo(
    () =>
      Platform.OS === "web"
        ? {
            inert: !drawerOpen,
            role: "dialog" as const,
            "aria-modal": drawerOpen,
          }
        : {},
    [drawerOpen],
  );

  const drawerContent = (
    <View
      {...drawerWebProps}
      ref={drawerRef}
      accessibilityElementsHidden={!drawerOpen}
      aria-hidden={!drawerOpen}
      importantForAccessibility={
        drawerOpen ? "yes" : "no-hide-descendants"
      }
      accessibilityViewIsModal={drawerOpen}
      accessibilityLabel="Navigation drawer"
      style={styles.drawerContent}
    >
      <PrimaryDrawerPanel
        closeButtonRef={closeButtonRef}
        drawerVisible={drawerOpen}
        onClose={closeDrawer}
        onClosePressIn={() => undefined}
        onNavigateAway={dismissDrawerForNavigation}
      />
    </View>
  );

  if (docked) {
    return (
      <PrimaryPageActionProvider>
        <PrimarySelectionBarProvider>
          <PrimarySurfaceInteractionProvider drawerPhase="closed" routeFocused={routeFocused}>
            <View style={[styles.root, styles.dockedRoot, { backgroundColor: colors.bgPrimary }]}>
              <View
                role="navigation"
                accessibilityLabel="Navigation"
                style={[styles.sidebar, { borderRightColor: colors.borderSubtle }]}
              >
                <PrimaryDrawerPanel
                  closeButtonRef={closeButtonRef}
                  drawerVisible
                  onClose={closeDrawer}
                  onClosePressIn={() => undefined}
                  onNavigateAway={() => undefined}
                  docked={{ activePrimaryRoute, onSelectPrimaryRoute }}
                />
              </View>
              <View ref={primaryRef} collapsable={false} style={styles.primary}>
                <PrimaryAppBar
                  activePrimaryRoute={activePrimaryRoute}
                  docked
                  drawerVisible={false}
                  menuButtonRef={menuButtonRef}
                  onOpenDrawer={openDrawer}
                  onOpenPressIn={beginOpenInteraction}
                  onSelectPrimaryRoute={onSelectPrimaryRoute}
                  topInset={insets.top}
                />
                <View style={styles.content}>{children}</View>
              </View>
            </View>
          </PrimarySurfaceInteractionProvider>
        </PrimarySelectionBarProvider>
      </PrimaryPageActionProvider>
    );
  }

  return (
    <PrimaryPageActionProvider>
      <PrimarySelectionBarProvider>
      <PrimarySurfaceInteractionProvider
        drawerPhase={drawerOpen ? "open" : "closed"}
        routeFocused={routeFocused}
      >
        <PrimaryDrawer
          controller={drawer}
          drawer={drawerContent}
          drawerStyle={{
            backgroundColor: colors.bgPrimary,
            borderRightColor: colors.borderSubtle,
            borderRightWidth: StyleSheet.hairlineWidth,
          }}
          drawerWidth={drawerWidth}
          edgeWidth={PRIMARY_DRAWER_SWIPE_EDGE_WIDTH}
          onDragStart={beginOpenInteraction}
          overlayAccessibilityLabel="Close navigation drawer"
          overlayColor={colors.modalBackdrop}
          style={[styles.root, { backgroundColor: colors.bgPrimary }]}
          // Brain is the pager's leading page, so a leading-edge swipe there
          // can only mean the drawer; on Sessions it pages back to Brain.
          // Closing swipes work on every page while the drawer is open.
          swipeToOpenEnabled={routeFocused && activePrimaryRoute === "brain"}
          windowWidth={windowWidth}
        >
          {/* Never flattened: toggling pointerEvents would otherwise create
              and remove this native view on every open/close, re-parenting
              the whole primary surface under in-flight touches. */}
          <View
            {...primaryWebProps}
            ref={primaryRef}
            collapsable={false}
            style={styles.primary}
            pointerEvents={drawerOpen ? "none" : "auto"}
            accessibilityElementsHidden={drawerOpen}
            aria-hidden={drawerOpen}
            importantForAccessibility={
              drawerOpen ? "no-hide-descendants" : "auto"
            }
          >
            <PrimaryAppBar
              activePrimaryRoute={activePrimaryRoute}
              docked={false}
              drawerVisible={drawerOpen}
              menuButtonRef={menuButtonRef}
              onOpenDrawer={openDrawer}
              onOpenPressIn={beginOpenInteraction}
              onSelectPrimaryRoute={onSelectPrimaryRoute}
              topInset={insets.top}
            />
            <View style={styles.content}>{children}</View>
          </View>
        </PrimaryDrawer>
      </PrimarySurfaceInteractionProvider>
      </PrimarySelectionBarProvider>
    </PrimaryPageActionProvider>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    overflow: "hidden",
  },
  primary: {
    flex: 1,
    position: "relative",
  },
  content: {
    flex: 1,
  },
  appBar: {
    width: "100%",
    flexDirection: "row",
    alignItems: "flex-end",
    justifyContent: "space-between",
    paddingHorizontal: 4,
    borderBottomWidth: StyleSheet.hairlineWidth,
    zIndex: 2,
  },
  appBarOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    left: 0,
    zIndex: 5,
  },
  selectionBarSlot: {
    flex: 1,
    alignSelf: "stretch",
    minWidth: 0,
  },
  drawerContent: {
    flex: 1,
  },
  dockedRoot: {
    flexDirection: "row",
  },
  sidebar: {
    width: PRIMARY_SIDEBAR_WIDTH,
    borderRightWidth: StyleSheet.hairlineWidth,
  },
  dockedTitle: {
    flex: 1,
    alignSelf: "center",
    paddingHorizontal: 20,
    fontFamily: Typography.displayFont,
    fontSize: 24,
    lineHeight: 30,
    letterSpacing: -0.6,
  },
});
