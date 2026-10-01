import React, { useMemo } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppTheme } from "../../constants/tokens";
import type { ResourceTelemetry } from "../../services/resourceTelemetry";
import { EmptyState } from "../ui/EmptyState";
import { ConsumersSection } from "./ResourceConsumersSection";
import { DiskSection, PressureSection } from "./ResourceDetailSections";
import { CpuSection, MemorySection, PressureHeadline } from "./ResourceOverviewSections";
import { RESOURCES_CONTENT_MAX_WIDTH, createResourceStyles } from "./resourceStyles";

export interface ResourcesViewProps {
  telemetry: ResourceTelemetry | null;
  loading: boolean;
  error: string | null;
  connected: boolean;
  hasServer: boolean;
  serverName?: string;
  onRetry(): void;
  onOpenSettings?(): void;
  /** Clock for "updated" copy; injected so fixtures render stably. */
  now?: number;
}

/** Calm machine overview: state, CPU, memory, waiting, disk, then who. */
export function ResourcesView({ telemetry, loading, error, connected, hasServer, serverName, onRetry, onOpenSettings, now }: ResourcesViewProps) {
  const { colors } = useAppTheme();
  const insets = useSafeAreaInsets();
  const styles = useMemo(() => createResourceStyles(colors), [colors]);

  if (!telemetry) {
    const state = !hasServer
      ? { title: "No current server", detail: "Pair a server in Settings to see its resources.", busy: false }
      : loading
        ? { title: connected ? "Reading the machine" : "Connecting to server", detail: null, busy: true }
        : !connected
          ? { title: "Server offline", detail: "Resources appear once the server reconnects.", busy: false }
          : { title: "Resources unavailable", detail: error ?? "This server did not report resources.", busy: false };
    return (
      <View style={[layout.fill, { backgroundColor: colors.bgPrimary, justifyContent: "center" }]}>
        <EmptyState
          title={state.title}
          detail={state.detail}
          icon="pulse-outline"
          busy={state.busy}
          action={!hasServer && onOpenSettings
            ? { label: "Open Settings", onPress: onOpenSettings }
            : !state.busy && connected
              ? { label: "Try again", icon: "refresh-outline", onPress: onRetry }
              : undefined}
        />
      </View>
    );
  }

  return (
    <ScrollView
      style={[layout.fill, { backgroundColor: colors.bgPrimary }]}
      contentContainerStyle={[layout.content, { paddingBottom: Math.max(insets.bottom, 20) + 24 }]}
      showsVerticalScrollIndicator={false}
    >
      {serverName ? <Text style={styles.caption}>{serverName}</Text> : null}
      {!connected || error ? (
        <EmptyState
          size="inline"
          title={!connected ? "Server offline" : "Refresh failed"}
          detail={!connected ? "Showing the last sample. Updates resume when the server reconnects." : error}
          action={connected ? { label: "Try again", icon: "refresh-outline", onPress: onRetry } : undefined}
        />
      ) : null}
      <PressureHeadline
        telemetry={telemetry}
        styles={styles}
        now={now ?? Date.now()}
        statusLabel={!connected || error ? "Last sample" : undefined}
      />
      <CpuSection telemetry={telemetry} styles={styles} />
      <MemorySection telemetry={telemetry} styles={styles} />
      <PressureSection telemetry={telemetry} styles={styles} />
      <DiskSection telemetry={telemetry} styles={styles} />
      <ConsumersSection telemetry={telemetry} styles={styles} />
    </ScrollView>
  );
}

const layout = StyleSheet.create({
  fill: { flex: 1 },
  content: {
    width: "100%",
    maxWidth: RESOURCES_CONTENT_MAX_WIDTH,
    alignSelf: "center",
    paddingHorizontal: 18,
    paddingTop: 12,
    gap: 12,
  },
});
