import React, { useCallback, type RefObject } from "react";
import {
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
  type View as ViewInstance,
} from "react-native";
import { useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { Typography, useAppTheme } from "../../constants/tokens";
import { useWorkerServerSummary, useWorkers } from "../../store/workers";
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
import { sessionsNeedYou } from "./primarySessionsAttention";

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
}

interface DrawerRowProps {
  drawerVisible: boolean;
  icon: IconName;
  label: string;
  onPress(): void;
}

/**
 * One navigation destination. Every row pushes a screen, so there is no
 * selected state. Seal & Slip: a bare soft-ink glyph and the label, no tile.
 */
function DrawerRow({ drawerVisible, icon, label, onPress }: DrawerRowProps) {
  const { colors } = useAppTheme();
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
      tabIndex={drawerVisible ? 0 : -1}
      android_ripple={{ color: colors.surfacePressed }}
      style={({ pressed }) => [
        styles.drawerRow,
        {
          backgroundColor: pressed ? colors.surfacePressed : "transparent",
        },
      ]}
    >
      <View style={styles.drawerRowIcon}>
        <Icon name={icon} color={colors.textSecondary} size={DRAWER_ICON_SIZE} />
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
  onPress,
}: {
  drawerVisible: boolean;
  label: string;
  icon: IconName;
  selected: boolean;
  attention?: boolean;
  onPress(): void;
}) {
  const { colors, theme } = useAppTheme();
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="tab"
      accessibilityLabel={attention ? `${label}, needs you` : label}
      accessibilityState={{ selected }}
      tabIndex={drawerVisible ? 0 : -1}
      android_ripple={{ color: colors.surfacePressed }}
      style={({ pressed }) => [
        styles.drawerRow,
        {
          backgroundColor: selected
            ? theme.materials.tint
            : pressed
              ? colors.surfacePressed
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

export function PrimaryDrawerPanel({
  closeButtonRef,
  drawerVisible,
  activePrimaryRoute,
  onSelectPrimaryRoute,
  onClose,
  onClosePressIn,
  onNavigateAway,
  docked = false,
}: PrimaryDrawerPanelProps) {
  const router = useRouter();
  const { colors } = useAppTheme();
  const { state: workersState } = useWorkers();
  const { serverConnections, serverConnectionIssues } = useWorkerServerSummary();
  const { currentServer } = useCurrentServer();
  const currentConnection = currentServer
    ? serverConnections[currentServer.id] || "offline"
    : "offline";
  const currentIssue = currentServer
    ? serverConnectionIssues[currentServer.id] || null
    : null;
  const connectionSummary = currentServer?.name || "No current server";
  const connectionDetail = !currentServer
    ? "Pair a server in Settings"
    : currentIssue?.title ??
      (currentConnection === "connected"
        ? "Connected"
        : currentConnection === "connecting"
          ? "Connecting"
          : "Offline");
  const sessionsAttention = sessionsNeedYou(workersState.workers, currentServer?.id);
  // Healthy is the quiet default; only a state the user can act on is colored.
  const connectionInk = currentIssue
    ? colors.dangerText
    : currentServer && currentConnection === "offline"
      ? colors.warning
      : colors.textTertiary;

  const openRoute = useCallback(
    (pathname: PrimaryDrawerPathname) => {
      onNavigateAway();
      router.push(pathname);
    },
    [onNavigateAway, router],
  );
  const selectPlace = useCallback(
    (route: PrimaryRouteName) => {
      if (!docked) onClose();
      onSelectPrimaryRoute(route);
    },
    [docked, onClose, onSelectPrimaryRoute],
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
        <View accessibilityRole="tablist" style={styles.drawerList}>
          {PRIMARY_DRAWER_PLACES.map((place) => (
            <PrimaryPlaceRow
              key={place.key}
              drawerVisible={drawerVisible}
              label={place.label}
              icon={place.icon}
              selected={activePrimaryRoute === place.route}
              attention={place.route === "list" && sessionsAttention}
              onPress={() => selectPlace(place.route)}
            />
          ))}
        </View>
        {PRIMARY_DRAWER_DESTINATIONS.map((destination) => (
          <DrawerRow
            key={destination.key}
            drawerVisible={drawerVisible}
            icon={destination.icon}
            label={destination.label}
            onPress={() => openRoute(destination.pathname)}
          />
        ))}
      </ScrollView>

      <View style={styles.drawerFooter}>
        <DrawerRow
          drawerVisible={drawerVisible}
          icon={PRIMARY_DRAWER_SETTINGS.icon}
          label={PRIMARY_DRAWER_SETTINGS.label}
          onPress={() => openRoute(PRIMARY_DRAWER_SETTINGS.pathname)}
        />
        {/* Where you are. Read-only: switching servers lives in Settings. */}
        <View
          accessible
          accessibilityLabel={`Current server, ${connectionSummary}, ${connectionDetail}`}
          style={[styles.serverStatus, { borderTopColor: colors.borderSubtle }]}
        >
          <View style={styles.drawerRowIcon}>
            <Icon name="desktop" size={16} color={colors.textTertiary} />
          </View>
          <Text
            numberOfLines={1}
            style={[styles.serverTitle, { color: colors.textSecondary }]}
          >
            {connectionSummary}
          </Text>
          <Text
            numberOfLines={1}
            style={[styles.serverDetail, { color: connectionInk }]}
          >
            {connectionDetail}
          </Text>
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
  serverStatus: {
    marginTop: 6,
    minHeight: 44,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 14,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  serverTitle: {
    flex: 1,
    minWidth: 0,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  serverDetail: {
    flexShrink: 1,
    maxWidth: "50%",
    fontSize: 12.5,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
});
