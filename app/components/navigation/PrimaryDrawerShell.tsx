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
  type ViewInstance,
} from "react-native";
import { useIsFocused } from "expo-router";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ContentWidth, Spacing, TypeScale, useAppColors } from "../../constants/tokens";
import { SIDEBAR_WIDTH, shouldDockSidebar } from "../../constants/windowRoom";
import { SESSIONS_TEXT_INSET } from "../workers/SessionsListView";
import type { PrimaryRouteName } from "../../services/interactionTrace";
import { Icon } from "../icons/Icon";
import { PrimaryChromeButton } from "./PrimaryChromeButton";
import {
  PrimaryAppBarPageAction,
  PrimaryPageActionProvider,
} from "./PrimaryPageAction";
import { PrimaryDrawer, usePrimaryDrawerController } from "./PrimaryDrawer";
import { PrimaryDrawerPanel } from "./PrimaryDrawerPanel";
import { PrimarySplitLayout } from "./PrimarySplitLayout";
import {
  PrimarySurfaceInteractionProvider,
} from "./PrimarySurfaceInteraction";
import { PrimaryTopSwitch } from "./PrimaryTopSwitch";
import {
  PrimarySelectionBarProvider,
  usePrimarySelectionBarContent,
} from "./PrimarySelectionBar";
import { resolvePrimaryAppBarGeometry } from "./primaryAppBarGeometry";
import { PrimaryAppBarLayoutContext, type PrimaryAppBarLayout } from "./primaryAppBarInset";
import { useDrawerFocusContainment } from "./useDrawerFocusContainment";
import { usePrimaryDrawerBack } from "./usePrimaryDrawerBack";
import { useDesktopWebShellHosted } from "./useDesktopWeb";
import { useConnectionAttention } from "./useConnectionAttention";
import { useAppUpdatePending } from "../../services/useAppUpdate";

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
  /** Docked: how the page's content sits under the bar. */
  layout?: PrimaryAppBarLayout | null;
}

function PrimaryAppBar({
  activePrimaryRoute,
  docked,
  drawerVisible,
  menuButtonRef,
  onOpenDrawer,
  onOpenPressIn,
  onSelectPrimaryRoute,
  topInset,
  layout,
}: PrimaryAppBarProps) {
  const colors = useAppColors();
  const geometry = resolvePrimaryAppBarGeometry(topInset);
  const showBrainCanvas = activePrimaryRoute === "brain";
  // The dot on ☰ is the connection signal on phones; healthy and a short
  // reconnect stay quiet (see connectionAttention). A waiting app update
  // shows the seal dot when the connection has nothing to say.
  const attention = useConnectionAttention();
  const updatePending = useAppUpdatePending();
  const menuBadge =
    attention.badge === "issue"
      ? colors.statusFailed
      : attention.badge === "offline"
        ? colors.warning
        : updatePending
          ? colors.seal
          : null;
  const menuLabel = updatePending ? `${attention.menuLabel}, update available` : attention.menuLabel;
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
          right: layout?.trailingInset ?? 0,
          backgroundColor: showBrainCanvas ? "transparent" : colors.bgPrimary,
          borderBottomColor: "transparent",
        },
      ]}
    >
      {docked ? (
        <View
          style={[
            styles.dockedRow,
            // The row is the page's own column: Brain's chat, or the Sessions
            // list, whose rows start SESSIONS_TEXT_INSET in.
            activePrimaryRoute === "brain"
              ? { maxWidth: ContentWidth.reading, paddingLeft: Spacing.lg - APP_BAR_SIDE_PADDING }
              : { maxWidth: ContentWidth.page, paddingLeft: SESSIONS_TEXT_INSET - APP_BAR_SIDE_PADDING },
          ]}
        >
          <Text
            accessibilityRole="header"
            numberOfLines={1}
            style={[styles.dockedTitle, { color: colors.textPrimary }]}
          >
            {activePrimaryRoute === "brain" ? "Brain" : "Sessions"}
          </Text>
          <PrimaryAppBarPageAction drawerVisible={drawerVisible} />
        </View>
      ) : (
      <>
      <PrimaryChromeButton
        ref={menuButtonRef}
        onPress={onOpenDrawer}
        onPressIn={onOpenPressIn}
        accessibilityRole="button"
        accessibilityLabel={menuLabel}
        tabIndex={drawerVisible ? -1 : 0}
        badgeColor={menuBadge}
      >
        <Icon name="menu" color={colors.textPrimary} size={20} />
      </PrimaryChromeButton>
      <PrimaryTopSwitch
        activeRoute={activePrimaryRoute}
        onSelectRoute={onSelectPrimaryRoute}
      />
      <PrimaryAppBarPageAction drawerVisible={drawerVisible} />
      </>
      )}
    </View>
  );
}

export function PrimaryDrawerShell({
  activePrimaryRoute,
  children,
  onSelectPrimaryRoute,
}: PrimaryDrawerShellProps) {
  const colors = useAppColors();
  const { width: windowWidth, height: windowHeight } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const routeFocused = useIsFocused();
  const drawerWidth = Math.min(320, Math.max(240, windowWidth - 52));
  // Room for a sidebar beside a page (see windowRoom): the drawer then docks.
  const docked = shouldDockSidebar(windowWidth, windowHeight);
  const [appBarLayouts, setAppBarLayouts] = useState<Partial<Record<PrimaryRouteName, PrimaryAppBarLayout>>>({});
  const setAppBarLayout = useCallback((route: PrimaryRouteName, layout: PrimaryAppBarLayout | null) => {
    setAppBarLayouts((current) => {
      if (!layout) {
        if (!(route in current)) return current;
        const { [route]: _removed, ...rest } = current;
        return rest;
      }
      return { ...current, [route]: layout };
    });
  }, []);
  // Desktop web: the root shell already draws the sidebar beside every page.
  const sidebarHosted = useDesktopWebShellHosted();
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
  // A docked sidebar has no drawer: whatever the controller still holds
  // (folding open mid-drag, say) is closed below and counts as closed here.
  const drawerOpen = drawer.open && !docked;
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

  const drawerHeldOpen = drawer.open;
  useEffect(() => {
    if (docked && drawerHeldOpen) {
      requestDrawer(false);
    }
  }, [docked, drawerHeldOpen, requestDrawer]);

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
        activePrimaryRoute={activePrimaryRoute}
        onSelectPrimaryRoute={onSelectPrimaryRoute}
        onClose={closeDrawer}
        onClosePressIn={() => undefined}
        onNavigateAway={dismissDrawerForNavigation}
      />
    </View>
  );

  const sidebar =
    docked && !sidebarHosted ? (
      <View
        role="navigation"
        accessibilityLabel="Navigation"
        style={[styles.sidebar, { borderRightColor: colors.borderSubtle }]}
      >
        <PrimaryDrawerPanel
          closeButtonRef={closeButtonRef}
          drawerVisible
          activePrimaryRoute={activePrimaryRoute}
          onSelectPrimaryRoute={onSelectPrimaryRoute}
          onClose={closeDrawer}
          onClosePressIn={() => undefined}
          onNavigateAway={() => undefined}
          docked
        />
      </View>
    ) : null;

  // One tree for both layouts: docking only fills the sidebar slot and the
  // drawer's own content, so the page below keeps its place and its state.
  return (
    <PrimaryAppBarLayoutContext.Provider value={setAppBarLayout}>
      <PrimaryPageActionProvider>
        <PrimarySelectionBarProvider>
          <PrimarySurfaceInteractionProvider
            drawerPhase={drawerOpen ? "open" : "closed"}
            routeFocused={routeFocused}
          >
            <PrimarySplitLayout
              sidebar={sidebar}
              style={{ backgroundColor: colors.bgPrimary }}
            >
              <PrimaryDrawer
                controller={drawer}
                drawer={docked ? null : drawerContent}
                drawerStyle={{
                  backgroundColor: colors.bgPrimary,
                  borderRightColor: colors.borderSubtle,
                  borderRightWidth: StyleSheet.hairlineWidth,
                }}
                drawerWidth={drawerWidth}
                onDragStart={beginOpenInteraction}
                overlayAccessibilityLabel="Close navigation drawer"
                overlayColor={colors.modalBackdrop}
                style={[styles.root, { backgroundColor: colors.bgPrimary }]}
                // Brain is the pager's leading page, so a rightward swipe anywhere
                // on it can only mean the drawer; on Sessions it pages back to
                // Brain. Closing swipes work on every page while the drawer is open.
                swipeToOpenEnabled={!docked && routeFocused && activePrimaryRoute === "brain"}
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
                    docked={docked}
                    drawerVisible={drawerOpen}
                    menuButtonRef={menuButtonRef}
                    onOpenDrawer={openDrawer}
                    onOpenPressIn={beginOpenInteraction}
                    onSelectPrimaryRoute={onSelectPrimaryRoute}
                    topInset={insets.top}
                    layout={docked ? appBarLayouts[activePrimaryRoute] : null}
                  />
                  <View style={styles.content}>{children}</View>
                </View>
              </PrimaryDrawer>
            </PrimarySplitLayout>
          </PrimarySurfaceInteractionProvider>
        </PrimarySelectionBarProvider>
      </PrimaryPageActionProvider>
    </PrimaryAppBarLayoutContext.Provider>
  );
}

const APP_BAR_SIDE_PADDING = 4;

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
    flexDirection: "row",
    alignItems: "flex-end",
    justifyContent: "space-between",
    paddingHorizontal: APP_BAR_SIDE_PADDING,
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
  sidebar: {
    width: SIDEBAR_WIDTH,
    borderRightWidth: StyleSheet.hairlineWidth,
  },
  dockedRow: {
    flex: 1,
    alignSelf: "stretch",
    flexDirection: "row",
    alignItems: "center",
    // The bar is a row: auto side margins center the page's column in it.
    marginHorizontal: "auto",
    // The bar already pads its sides; the title's left padding is set per page.
    paddingRight: Spacing.lg - APP_BAR_SIDE_PADDING,
  },
  dockedTitle: {
    ...TypeScale.pageTitle,
    flex: 1,
    minWidth: 0,
  },
});
