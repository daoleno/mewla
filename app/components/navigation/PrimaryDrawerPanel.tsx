import React, { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import {
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
  type View as ViewInstance,
} from "react-native";
import * as Haptics from "expo-haptics";
import { useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { Typography, useAppTheme } from "../../constants/tokens";
import { useWorkers } from "../../store/workers";
import type { PrimaryRouteName } from "../../services/interactionTrace";
import { useCurrentServer } from "../../store/currentServer";
import { Icon, type IconName } from "../icons/Icon";
import { MewlaMark } from "../mewla/MewlaMark";
import {
  PRIMARY_DRAWER_DESTINATIONS,
  PRIMARY_DRAWER_PLACES,
  PRIMARY_DRAWER_SETTINGS,
  type PrimaryDrawerPathname,
} from "./primaryDrawerDestinations";
import { brainNeedsYou, sessionsNeedYou } from "./primarySessionsAttention";
import { useBrain } from "../../store/brain";
import { useConnectionAttention } from "./useConnectionAttention";
import { useAppUpdatePending } from "../../services/useAppUpdate";
import type { DesktopSidebarKey } from "./desktopWeb";
import { desktopShortcutTooltip } from "./desktopShortcuts";
import { useWebTooltip } from "../ui/useWebTooltip";
import { useToast } from "../ui/Toast";
import { switchToComputer } from "../../services/computerSwitch";
import { ComputerSwitcher, useToneInk } from "./ComputerSwitcher";
import {
  attentionTone,
  computerRowLabel,
  computerRowTap,
  computerSwitcherRows,
  type ComputerTone,
} from "./computerSwitcherModel";
import { useComputerChecks } from "./useComputerChecks";

interface PrimaryDrawerPanelProps {
  closeButtonRef: RefObject<ViewInstance | null>;
  drawerVisible: boolean;
  activePrimaryRoute: PrimaryRouteName;
  onSelectPrimaryRoute(route: PrimaryRouteName): void;
  onClose(): void;
  onClosePressIn(): void;
  onNavigateAway(): void;
  /** Wide layouts dock the panel as a permanent sidebar without a close button. */
  docked?: boolean;
  /**
   * Desktop web: the row that owns the current path, Brain and Sessions
   * included. Without it only Brain and Sessions show selection.
   */
  selectedKey?: DesktopSidebarKey | null;
  /** Desktop web opens rows through the shell; the default pushes. */
  onOpenPath?(pathname: PrimaryDrawerPathname): void;
}

interface DrawerRowProps {
  drawerVisible: boolean;
  icon: IconName;
  label: string;
  selected?: boolean;
  /** The seal dot: what behind this row needs you, read as the row's hint. */
  attention?: string | null;
  tooltip?: string;
  onPress(): void;
}

/**
 * One navigation destination. On phone a row pushes a screen, so there is no
 * selected state; the desktop web sidebar selects the row you are on.
 * Seal & Slip: a bare soft-ink glyph and the label, no tile.
 */
function DrawerRow({ drawerVisible, icon, label, selected = false, attention, tooltip, onPress }: DrawerRowProps) {
  const { colors, theme } = useAppTheme();
  const rowRef = useRef<ViewInstance>(null);
  useWebTooltip(rowRef, tooltip);
  return (
    <Pressable
      ref={rowRef}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityHint={attention ?? undefined}
      accessibilityState={selected ? { selected } : undefined}
      aria-current={selected ? "page" : undefined}
      tabIndex={drawerVisible ? 0 : -1}
      android_ripple={{ color: colors.surfacePressed }}
      style={(state) => [
        styles.drawerRow,
        {
          backgroundColor: selected
            ? theme.materials.tint
            : state.pressed
              ? colors.surfacePressed
              : (state as { hovered?: boolean }).hovered
                ? colors.surfaceSubtle
                : "transparent",
        },
      ]}
    >
      <View style={styles.drawerRowIcon}>
        <Icon name={icon} color={selected ? colors.textPrimary : colors.textSecondary} size={DRAWER_ICON_SIZE} />
      </View>
      <Text
        numberOfLines={1}
        style={[
          styles.drawerRowLabel,
          {
            color: colors.textPrimary,
            fontFamily: Typography.uiFontMedium,
          },
        ]}
      >
        {label}
      </Text>
      {attention ? <View style={[styles.sealDot, { backgroundColor: colors.seal }]} /> : null}
    </Pressable>
  );
}

/** Brain and Sessions: the two primary pages, selected in place. */
function PrimaryPlaceRow({
  drawerVisible,
  label,
  icon,
  selected,
  attention,
  tooltip,
  onPress,
}: {
  drawerVisible: boolean;
  label: string;
  icon: IconName;
  selected: boolean;
  attention?: boolean;
  tooltip?: string;
  onPress(): void;
}) {
  const { colors, theme } = useAppTheme();
  const rowRef = useRef<ViewInstance>(null);
  useWebTooltip(rowRef, tooltip);
  return (
    <Pressable
      ref={rowRef}
      onPress={onPress}
      accessibilityRole="tab"
      accessibilityLabel={attention ? `${label}, needs you` : label}
      accessibilityState={{ selected }}
      tabIndex={drawerVisible ? 0 : -1}
      android_ripple={{ color: colors.surfacePressed }}
      style={(state) => [
        styles.drawerRow,
        {
          backgroundColor: selected
            ? theme.materials.tint
            : state.pressed
              ? colors.surfacePressed
              : (state as { hovered?: boolean }).hovered
                ? colors.surfaceSubtle
                : "transparent",
        },
      ]}
    >
      <View style={styles.drawerRowIcon}>
        <Icon
          name={icon}
          size={DRAWER_ICON_SIZE}
          color={selected ? colors.textPrimary : colors.textSecondary}
        />
      </View>
      <Text
        numberOfLines={1}
        style={[styles.drawerRowLabel, { color: colors.textPrimary, fontFamily: Typography.uiFontMedium }]}
      >
        {label}
      </Text>
      {attention ? <View style={[styles.sealDot, { backgroundColor: colors.seal }]} /> : null}
    </Pressable>
  );
}

/**
 * The computer in use, at the foot of the menu: its name, and a state word
 * only when there is something to act on (offline, an issue). Tapping it
 * switches computer, or opens Settings → Computers when there is nothing to
 * switch between.
 */
function ComputerRow({
  drawerVisible,
  name,
  status,
  tone,
  switchable,
  expanded,
  onPress,
}: {
  drawerVisible: boolean;
  name: string;
  status: string;
  tone: ComputerTone;
  switchable: boolean;
  expanded: boolean;
  onPress(): void;
}) {
  const { colors } = useAppTheme();
  const toneInk = useToneInk()(tone);
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`Computer, ${computerRowLabel({ name, status, current: false })}`}
      accessibilityHint={switchable ? "Switch computer" : "Open computers in Settings"}
      accessibilityState={switchable ? { expanded } : undefined}
      aria-haspopup={switchable ? "menu" : undefined}
      tabIndex={drawerVisible ? 0 : -1}
      android_ripple={{ color: colors.surfacePressed }}
      style={(state) => [
        styles.serverStatus,
        {
          backgroundColor: state.pressed
            ? colors.surfacePressed
            : (state as { hovered?: boolean }).hovered
              ? colors.surfaceSubtle
              : "transparent",
        },
      ]}
    >
      <View style={styles.drawerRowIcon}>
        <Icon name="desktop" size={DRAWER_ICON_SIZE} color={toneInk ?? colors.textSecondary} />
      </View>
      <Text numberOfLines={1} style={[styles.serverTitle, { color: colors.textPrimary }]}>
        {name}
      </Text>
      {status ? (
        <View style={styles.serverState}>
          <View style={[styles.sealDot, { backgroundColor: toneInk ?? colors.textTertiary }]} />
          <Text numberOfLines={1} style={[styles.serverDetail, { color: toneInk ?? colors.textTertiary }]}>
            {status}
          </Text>
        </View>
      ) : null}
      {switchable ? <Icon name="swap-horizontal" size={16} color={colors.textTertiary} /> : null}
    </Pressable>
  );
}

export function PrimaryDrawerPanel({
  closeButtonRef,
  drawerVisible,
  activePrimaryRoute,
  onSelectPrimaryRoute,
  onClose,
  onClosePressIn,
  onNavigateAway,
  docked = false,
  selectedKey,
  onOpenPath,
}: PrimaryDrawerPanelProps) {
  const router = useRouter();
  const { colors } = useAppTheme();
  const { state: workersState } = useWorkers();
  const { currentServer, servers, switchCurrentServer } = useCurrentServer();
  const attention = useConnectionAttention();
  const toast = useToast();
  const [switcherOpen, setSwitcherOpen] = useState(false);
  const checks = useComputerChecks(servers, currentServer?.id, switcherOpen);
  const updatePending = useAppUpdatePending();
  const computerName = currentServer?.name || "No computer";
  const rows = computerSwitcherRows({
    servers,
    currentServerId: currentServer?.id,
    attention,
    checks,
  });
  const currentRow = rows.find((row) => row.current);
  const switchable = computerRowTap(servers.length) === "switcher";
  const sessionsAttention = sessionsNeedYou(workersState.workers, currentServer?.id);
  const { state: brainState } = useBrain();
  const brainAttention = brainNeedsYou(
    currentServer ? brainState.byServer[currentServer.id]?.current_work : undefined,
  );
  // Shortcut tooltips belong to the desktop web sidebar only.
  const shortcutTip = (label: string, key: string) =>
    selectedKey === undefined ? undefined : desktopShortcutTooltip(label, key);
  const openRoute = useCallback(
    (pathname: PrimaryDrawerPathname) => {
      onNavigateAway();
      if (onOpenPath) onOpenPath(pathname);
      else router.push(pathname);
    },
    [onNavigateAway, onOpenPath, router],
  );
  const selectPlace = useCallback(
    (route: PrimaryRouteName) => onSelectPrimaryRoute(route),
    [onSelectPrimaryRoute],
  );

  // A closed drawer takes its switcher with it.
  useEffect(() => {
    if (!drawerVisible) setSwitcherOpen(false);
  }, [drawerVisible]);
  const closeSwitcher = useCallback(() => setSwitcherOpen(false), []);
  const pressComputer = useCallback(() => {
    if (computerRowTap(servers.length) === "switcher") setSwitcherOpen((open) => !open);
    else openRoute(PRIMARY_DRAWER_SETTINGS.pathname);
  }, [openRoute, servers.length]);
  // The same switch Settings makes; the drawer then shows that computer's page.
  const selectComputer = useCallback(
    (id: string) => {
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
      onNavigateAway();
      void switchToComputer(id, switchCurrentServer).catch((error) => {
        toast.show({
          title: "Couldn't switch computers",
          detail: error instanceof Error ? error.message : "Unable to switch to that computer.",
          tone: "error",
        });
      });
    },
    [onNavigateAway, switchCurrentServer, toast],
  );

  return (
    <SafeAreaView style={styles.drawerContent} edges={["top", "bottom"]}>
      <View style={styles.drawerIdentity}>
        <MewlaMark size={26} />
        <Text
          style={[
            styles.drawerTitle,
            {
              color: colors.textPrimary,
              fontFamily: Typography.displayFont,
            },
          ]}
          accessibilityRole="header"
        >
          Mewla
        </Text>
        {docked ? null : <Pressable
          ref={closeButtonRef}
          onPress={onClose}
          onPressIn={onClosePressIn}
          accessibilityRole="button"
          accessibilityLabel="Close navigation drawer"
          tabIndex={drawerVisible ? 0 : -1}
          hitSlop={6}
          style={({ pressed }) => [
            styles.closeButton,
            {
              backgroundColor: pressed
                ? colors.surfacePressed
                : "transparent",
            },
          ]}
        >
          <Icon name="close" color={colors.textSecondary} size={18} />
        </Pressable>}
      </View>

      <ScrollView
        style={styles.drawerScroll}
        contentContainerStyle={styles.drawerList}
        showsVerticalScrollIndicator={false}
        keyboardShouldPersistTaps="handled"
      >
        {/* One home per layout: the phone's app bar switches Brain and Sessions,
            so only the docked sidebar lists them. */}
        {docked ? <View accessibilityRole="tablist" style={styles.drawerList}>
          {PRIMARY_DRAWER_PLACES.map((place) => (
            <PrimaryPlaceRow
              key={place.key}
              drawerVisible={drawerVisible}
              label={place.label}
              icon={place.icon}
              selected={
                selectedKey === undefined
                  ? activePrimaryRoute === place.route
                  : selectedKey === place.key
              }
              // Brain's dot points elsewhere, so it hides on Brain itself.
              attention={place.route === "list" ? sessionsAttention : place.route === "brain" && brainAttention && (selectedKey === undefined ? activePrimaryRoute !== place.route : selectedKey !== place.key)}
              tooltip={shortcutTip(place.label, place.key)}
              onPress={() => selectPlace(place.route)}
            />
          ))}
        </View> : null}
        {PRIMARY_DRAWER_DESTINATIONS.map((destination) => (
          <DrawerRow
            key={destination.key}
            drawerVisible={drawerVisible}
            icon={destination.icon}
            label={destination.label}
            selected={selectedKey === destination.key}
            tooltip={shortcutTip(destination.label, destination.key)}
            onPress={() => openRoute(destination.pathname)}
          />
        ))}
      </ScrollView>

      <View style={styles.drawerFooter}>
        <DrawerRow
          drawerVisible={drawerVisible}
          icon={PRIMARY_DRAWER_SETTINGS.icon}
          label={PRIMARY_DRAWER_SETTINGS.label}
          selected={selectedKey === PRIMARY_DRAWER_SETTINGS.key}
          // A newer Mewla waits in Settings → About.
          attention={updatePending ? "Update available" : null}
          tooltip={shortcutTip(PRIMARY_DRAWER_SETTINGS.label, PRIMARY_DRAWER_SETTINGS.key)}
          onPress={() => openRoute(PRIMARY_DRAWER_SETTINGS.pathname)}
        />
        <View style={[styles.serverFooter, { borderTopColor: colors.borderSubtle }]}>
          <ComputerSwitcher
            visible={switcherOpen}
            rows={rows}
            manageIcon={PRIMARY_DRAWER_SETTINGS.icon}
            docked={docked}
            onSelect={selectComputer}
            onManage={() => openRoute(PRIMARY_DRAWER_SETTINGS.pathname)}
            onClose={closeSwitcher}
          />
          <ComputerRow
            drawerVisible={drawerVisible}
            name={computerName}
            status={currentRow?.status ?? ""}
            tone={currentRow?.tone ?? attentionTone(attention.badge)}
            switchable={switchable}
            expanded={switcherOpen}
            onPress={pressComputer}
          />
        </View>
      </View>
    </SafeAreaView>
  );
}

const DRAWER_ICON_SIZE = 20;
const DRAWER_ICON_SLOT = 24;

const styles = StyleSheet.create({
  drawerContent: {
    flex: 1,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  drawerIdentity: {
    minHeight: 56,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingLeft: 8,
  },
  drawerScroll: {
    flex: 1,
    minHeight: 0,
  },
  drawerList: {
    gap: 2,
  },
  drawerFooter: {
    flexShrink: 0,
    paddingTop: 8,
  },
  drawerTitle: {
    flex: 1,
    fontSize: 21,
    lineHeight: 28,
    letterSpacing: -0.4,
  },
  closeButton: {
    width: 44,
    height: 44,
    borderRadius: 22,
    alignItems: "center",
    justifyContent: "center",
  },
  drawerRow: {
    minHeight: 52,
    paddingHorizontal: 12,
    borderRadius: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 14,
  },
  drawerRowIcon: {
    width: DRAWER_ICON_SLOT,
    alignItems: "center",
    justifyContent: "center",
  },
  drawerRowLabel: {
    flex: 1,
    fontSize: 15,
    lineHeight: 22,
  },
  sealDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  serverFooter: {
    marginTop: 6,
    paddingTop: 6,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  serverStatus: {
    minHeight: 52,
    paddingHorizontal: 12,
    borderRadius: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 14,
  },
  serverTitle: {
    flex: 1,
    minWidth: 0,
    fontSize: 15,
    lineHeight: 22,
    fontFamily: Typography.uiFontMedium,
  },
  serverState: {
    flexShrink: 1,
    maxWidth: "62%",
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  serverDetail: {
    flexShrink: 1,
    fontSize: 12.5,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
});
