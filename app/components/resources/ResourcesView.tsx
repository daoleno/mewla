import React, { useMemo, useState } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppTheme } from "../../constants/tokens";
import { historyWindowLabel, type ResourceTelemetry } from "../../services/resourceTelemetry";
import { AnimatedPressable } from "../ui/AnimatedPressable";
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

/** Shared native dashboard; layout responds to available content width. */
export function ResourcesView({ telemetry, loading, error, connected, hasServer, serverName, onRetry, onOpenSettings, now }: ResourcesViewProps) {
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
      <View onLayout={(event) => setWidth(event.nativeEvent.layout.width)} style={{ gap: 10 }}>
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
          serverName={serverName} onRetry={onRetry} loading={loading} connected={connected}
        />
        {telemetry.signals.length > 0 ? <View style={styles.filterRow}>
          {telemetry.signals.map((signal) => <View key={signal.name} style={[styles.filter, { borderColor: colors.warning, minHeight: 28 }]}>
            <Text style={[styles.caption, { color: signal.state === "critical" ? colors.dangerText : colors.warning }]}>
              {signal.name} · {Number(signal.value.toFixed(2))} / threshold {signal.threshold}
            </Text>
          </View>)}
        </View> : null}
        <View style={styles.sectionHeader}>
          <Text style={styles.caption}>{historyWindowLabel(telemetry) ?? "Collecting history"} · CPU / memory 0–100%</Text>
          <AnimatedPressable style={styles.control} accessibilityRole="button" accessibilityState={{ expanded: details }}
            accessibilityLabel="Toggle machine details" onPress={() => setDetails((value) => !value)}>
            <Text style={styles.label}>{details ? "Less −" : "Details +"}</Text>
          </AnimatedPressable>
        </View>
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
