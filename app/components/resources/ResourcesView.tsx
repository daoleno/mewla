import React, { useMemo } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
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
  onRetry(): void;
  /** Clock for "updated" copy; injected so fixtures render stably. */
  now?: number;
}

/** Calm machine overview: state, CPU, memory, waiting, disk, then who. */
export function ResourcesView({ telemetry, loading, error, connected, hasServer, onRetry, now }: ResourcesViewProps) {
  const { colors } = useAppTheme();
  const insets = useSafeAreaInsets();
  const styles = useMemo(() => createResourceStyles(colors), [colors]);

  if (!telemetry) {
    const state = !hasServer
      ? { title: "No current server", detail: "Pair a server in Settings to see its resources.", busy: false }
      : !connected
        ? { title: "Server offline", detail: "Resources appear once the server reconnects.", busy: loading }
        : loading
          ? { title: "Reading the machine", detail: null, busy: true }
          : { title: "Resources unavailable", detail: error ?? "This server did not report resources.", busy: false };
    return (
      <View style={[layout.fill, { backgroundColor: colors.bgPrimary, justifyContent: "center" }]}>
        <EmptyState
          title={state.title}
          detail={state.detail}
          icon="pulse-outline"
          busy={state.busy}
          action={!state.busy && hasServer && connected ? { label: "Try again", icon: "refresh-outline", onPress: onRetry } : undefined}
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
      <PressureHeadline telemetry={telemetry} styles={styles} now={now ?? Date.now()} />
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
