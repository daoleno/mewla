import React, { useMemo, useState } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppTheme } from "../../constants/tokens";
import type { ResourceTelemetry } from "../../services/resourceTelemetry";
import { EmptyState } from "../ui/EmptyState";
import { ConsumersSection } from "./ResourceConsumersSection";
import { DiskSection, PressureSection } from "./ResourceDetailSections";
import { CpuSection, MemorySection, PressureHeadline, PressureSignals } from "./ResourceOverviewSections";
import { RESOURCES_CONTENT_MAX_WIDTH, createResourceStyles } from "./resourceStyles";

export interface ResourcesViewProps {
  telemetry: ResourceTelemetry | null;
  loading: boolean;
  error: string | null;
  connected: boolean;
  hasServer: boolean;
  onRetry(): void;
  onOpenSettings?(): void;
  /** Clock for "updated" copy; injected so fixtures render stably. */
  now?: number;
}

/** Shared native dashboard; layout responds to available content width. */
export function ResourcesView({ telemetry, loading, error, connected, hasServer, onRetry, onOpenSettings, now }: ResourcesViewProps) {
  const { colors } = useAppTheme();
  const insets = useSafeAreaInsets();
  const [width, setWidth] = useState(360);
  const [details, setDetails] = useState(false);
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
          icon="pulse"
          busy={state.busy}
          action={!hasServer && onOpenSettings
            ? { label: "Open Settings", onPress: onOpenSettings }
            : !state.busy && connected
              ? { label: "Try again", icon: "refresh", onPress: onRetry }
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
      <View onLayout={(event) => setWidth(event.nativeEvent.layout.width)} style={{ gap: 10 }}>
        {/* Offline, the headline caption alone says so; a failed refresh keeps its Retry. */}
        {connected && error ? (
          <EmptyState
            size="inline"
            title="Refresh failed"
            detail={error}
            action={{ label: "Try again", icon: "refresh", onPress: onRetry }}
          />
        ) : null}
        <PressureHeadline
          telemetry={telemetry}
          styles={styles}
          now={now ?? Date.now()}
          statusLabel={!connected ? "Last sample · offline" : error ? "Last sample" : undefined}
          onRetry={onRetry} loading={loading} connected={connected}
          details={details} onToggleDetails={() => setDetails((value) => !value)}
        />
        <PressureSignals telemetry={telemetry} styles={styles} />
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
          {[CpuSection, MemorySection, DiskSection, PressureSection].map((Section, index) => <View key={index}
            style={{ flexGrow: 1, flexBasis: width >= 1040 ? "23%" : width < 340 ? "100%" : "48%" }}>
            <Section telemetry={telemetry} styles={styles} details={details} />
          </View>)}
        </View>
        <ConsumersSection telemetry={telemetry} styles={styles} wide={width >= 760} />
      </View>
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
