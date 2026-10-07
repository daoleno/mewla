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
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import { Typography, useAppTheme } from "../../constants/tokens";
import { appVersion } from "../../constants/appVersion";
import { useWorkerServerSummary, useWorkers } from "../../store/workers";
import type { PrimaryRouteName } from "../../services/interactionTrace";
import { useCurrentServer } from "../../store/currentServer";
import { MewlaMark } from "../mewla/MewlaMark";
import {
  NavCloseIcon,
  NavPluginsIcon,
  NavResourcesIcon,
  NavSettingsIcon,
  NavSkillsIcon,
  NavStatsIcon,
} from "./PrimaryNavIcons";
import {
  PRIMARY_DRAWER_GROUP_CAPTIONS,
  PRIMARY_DRAWER_GROUPS,
  type PrimaryDrawerIcon,
  type PrimaryDrawerPathname,
} from "./primaryDrawerDestinations";
import { sessionsNeedYou } from "./primarySessionsAttention";

interface PrimaryDrawerPanelProps {
  closeButtonRef: RefObject<ViewInstance | null>;
  drawerVisible: boolean;
  onClose(): void;
  onClosePressIn(): void;
  onNavigateAway(): void;
  /**
   * Wide layouts dock the panel as a permanent sidebar: no close button, and
   * Brain and Sessions lead as its first rows.
   */
  docked?: {
    activePrimaryRoute: PrimaryRouteName;
    onSelectPrimaryRoute(route: PrimaryRouteName): void;
  };
}

interface DrawerRowProps {
  drawerVisible: boolean;
  icon: PrimaryDrawerIcon;
  label: string;
  onPress(): void;
}

const DRAWER_ROW_ICONS = {
  calendar: ({ color, size }: { color: string; size: number }) => (
    <Ionicons name="calendar-outline" color={color} size={size} />
  ),
  plugins: NavPluginsIcon,
  skills: NavSkillsIcon,
  stats: NavStatsIcon,
  browser: ({ color, size }: { color: string; size: number }) => (
    <Ionicons name="globe-outline" color={color} size={size} />
  ),
  resources: NavResourcesIcon,
  settings: NavSettingsIcon,
} satisfies Record<PrimaryDrawerIcon, unknown>;

/**
 * One navigation destination. Every row pushes a screen, so there is no
 * selected state. Seal & Slip: a bare soft-ink glyph and the label, no tile.
 */
function DrawerRow({ drawerVisible, icon, label, onPress }: DrawerRowProps) {
  const { colors } = useAppTheme();
  const Icon = DRAWER_ROW_ICONS[icon];
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
        <Icon color={colors.textSecondary} size={20} />
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

/** Docked sidebar only: Brain and Sessions, the two primary places. */
function PrimaryPlaceRow({
  label,
  icon,
  selected,
  attention,
  onPress,
}: {
  label: string;
  icon: React.ComponentProps<typeof Ionicons>["name"];
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
        <Ionicons name={icon} size={20} color={selected ? colors.textPrimary : colors.textSecondary} />
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
  onClose,
  onClosePressIn,
  onNavigateAway,
  docked,
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
          <NavCloseIcon color={colors.textSecondary} size={18} />
        </Pressable>}
      </View>

      <ScrollView
        style={styles.drawerScroll}
        showsVerticalScrollIndicator={false}
        keyboardShouldPersistTaps="handled"
      >
        {docked ? (
          <View accessibilityRole="tablist" style={styles.places}>
            <PrimaryPlaceRow
              label="Brain"
              icon="chatbubble-ellipses-outline"
              selected={docked.activePrimaryRoute === "brain"}
              onPress={() => docked.onSelectPrimaryRoute("brain")}
            />
            <PrimaryPlaceRow
              label="Sessions"
              icon="terminal-outline"
              selected={docked.activePrimaryRoute === "list"}
              attention={sessionsAttention}
              onPress={() => docked.onSelectPrimaryRoute("list")}
            />
          </View>
        ) : null}
        {/* Where you are. Read-only: switching servers lives in Settings. */}
        <View
          accessible
          accessibilityLabel={`Current server, ${connectionSummary}, ${connectionDetail}`}
          style={[styles.serverHeader, { backgroundColor: colors.bgSurface, borderColor: colors.borderSubtle }]}
        >
          <View style={styles.serverGlyph}>
            <Ionicons name="desktop-outline" size={20} color={colors.textSecondary} />
          </View>
          <View style={styles.serverCopy}>
            <Text
              numberOfLines={1}
              style={[styles.serverTitle, { color: colors.textPrimary }]}
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

        {PRIMARY_DRAWER_GROUPS.map((group, groupIndex) => (
          <View key={group[0]?.key ?? groupIndex} style={styles.drawerGroup}>
            <Text
              accessibilityRole="header"
              style={[styles.groupCaption, { color: colors.textTertiary }]}
            >
              {PRIMARY_DRAWER_GROUP_CAPTIONS[groupIndex]}
            </Text>
            {group.map((destination) => (
              <DrawerRow
                key={destination.key}
                drawerVisible={drawerVisible}
                icon={destination.icon}
                label={destination.label}
                onPress={() => openRoute(destination.pathname)}
              />
            ))}
          </View>
        ))}
      </ScrollView>

      <View style={styles.drawerFooter}>
        <Text
          style={[
            styles.drawerVersion,
            {
              color: colors.textTertiary,
              fontFamily: Typography.terminalFont,
            },
          ]}
        >
          Mewla v{appVersion}
        </Text>
      </View>
    </SafeAreaView>
  );
}

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
  places: {
    marginTop: 4,
    gap: 2,
  },
  serverHeader: {
    marginTop: 12,
    minHeight: 56,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
  },
  serverGlyph: {
    width: DRAWER_ICON_SLOT,
    alignItems: "center",
    justifyContent: "center",
  },
  serverCopy: {
    flex: 1,
    minWidth: 0,
    gap: 1,
  },
  serverTitle: {
    fontSize: 15,
    lineHeight: 21,
    fontFamily: Typography.uiFontMedium,
  },
  serverDetail: {
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  drawerGroup: {
    marginTop: 14,
  },
  groupCaption: {
    paddingHorizontal: 12,
    paddingBottom: 4,
    fontSize: 12.5,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
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
  drawerVersion: {
    paddingVertical: 8,
    textAlign: "center",
    fontSize: 11,
    lineHeight: 15,
  },
});
