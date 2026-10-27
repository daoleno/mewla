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
import { DESKTOP_PAGE_GUTTER, TypeScale, useAppColors } from "../../constants/tokens";
import type { PrimaryRouteName } from "../../services/interactionTrace";
import { Icon } from "../icons/Icon";
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
            // The title sits at the page gutter on every page; a column page's
            // action ends where its column ends.
            layout?.column
              ? { maxWidth: DESKTOP_PAGE_GUTTER - APP_BAR_SIDE_PADDING - (layout.columnInset ?? 0) + layout.column }
              : null,
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
  const { width: windowWidth } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const routeFocused = useIsFocused();
  const drawerWidth = Math.min(320, Math.max(240, windowWidth - 52));
  const docked = windowWidth >= PRIMARY_SIDEBAR_BREAKPOINT;
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
        activePrimaryRoute={activePrimaryRoute}
        onSelectPrimaryRoute={onSelectPrimaryRoute}
        onClose={closeDrawer}
        onClosePressIn={() => undefined}
        onNavigateAway={dismissDrawerForNavigation}
      />
    </View>
  );

  if (docked) {
    return (
      <PrimaryAppBarLayoutContext.Provider value={setAppBarLayout}>
      <PrimaryPageActionProvider>
        <PrimarySelectionBarProvider>
          <PrimarySurfaceInteractionProvider drawerPhase="closed" routeFocused={routeFocused}>
            <View style={[styles.root, styles.dockedRoot, { backgroundColor: colors.bgPrimary }]}>
              {sidebarHosted ? null : <View
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
              </View>}
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
                  layout={appBarLayouts[activePrimaryRoute]}
                />
                <View style={styles.content}>{children}</View>
              </View>
            </View>
          </PrimarySurfaceInteractionProvider>
        </PrimarySelectionBarProvider>
      </PrimaryPageActionProvider>
      </PrimaryAppBarLayoutContext.Provider>
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
          onDragStart={beginOpenInteraction}
          overlayAccessibilityLabel="Close navigation drawer"
          overlayColor={colors.modalBackdrop}
          style={[styles.root, { backgroundColor: colors.bgPrimary }]}
          // Brain is the pager's leading page, so a rightward swipe anywhere
          // on it can only mean the drawer; on Sessions it pages back to
          // Brain. Closing swipes work on every page while the drawer is open.
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
  dockedRoot: {
    flexDirection: "row",
  },
  sidebar: {
    width: PRIMARY_SIDEBAR_WIDTH,
    borderRightWidth: StyleSheet.hairlineWidth,
  },
  dockedRow: {
    flex: 1,
    alignSelf: "stretch",
    flexDirection: "row",
    alignItems: "center",
    width: "100%",
    // The bar already pads its sides.
    paddingLeft: DESKTOP_PAGE_GUTTER - APP_BAR_SIDE_PADDING,
  },
  dockedTitle: {
    ...TypeScale.pageTitle,
    flex: 1,
    minWidth: 0,
  },
});
