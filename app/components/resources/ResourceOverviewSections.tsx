import React from "react";
import { Text, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import {
  chartWindow, cpuHistorySeries, formatBytes, formatPercent, memoryHistorySeries,
  memoryUsedRatio, pressureSpans, sampledAgoLabel, type ResourceTelemetry,
} from "../../services/resourceTelemetry";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { AreaChart, CoreBars, PressureStrip, StackBar, pressureInk } from "./ResourceCharts";
import type { ResourceStyles } from "./resourceStyles";

export interface SectionProps {
  telemetry: ResourceTelemetry;
  styles: ResourceStyles;
  details?: boolean;
}

export function PressureHeadline({ telemetry, styles, now, statusLabel, serverName, onRetry, loading, connected }: SectionProps & {
  now: number; statusLabel?: string; serverName?: string; onRetry(): void; loading: boolean; connected: boolean;
}) {
  const { colors } = useAppTheme();
  return (
    <View style={styles.sectionHeader}>
      <View style={{ flex: 1, gap: 4 }}>
        <View style={styles.legendRow}>
          <Text style={styles.sectionTitle} accessibilityRole="header">{serverName ?? "Machine"}</Text>
          <Text style={[styles.label, { color: pressureInk(telemetry.state, colors) }]}>
            {telemetry.state === "normal" ? "Normal" : telemetry.state === "elevated" ? "Elevated" : "Critical"}
          </Text>
        </View>
        <Text style={styles.caption}>
          {statusLabel ?? sampledAgoLabel(telemetry.sampledAt, now)} · {new Date(telemetry.sampledAt).toLocaleTimeString()} · 5s poll
        </Text>
      </View>
      <AnimatedPressable accessibilityRole="button" accessibilityLabel="Refresh resources" disabled={loading || !connected}
        onPress={onRetry} style={styles.control}>
        <Text style={styles.label}>{loading ? "Refreshing…" : "Refresh"}</Text>
      </AnimatedPressable>
    </View>
  );
}

export function CpuSection({ telemetry, styles, details }: SectionProps) {
  const { colors } = useAppTheme();
  const series = cpuHistorySeries(telemetry);
  const window = chartWindow(telemetry);
  const cores = telemetry.cpu.perCorePercent;
  return (
    <View style={styles.surface}>
      <Text style={styles.label}>CPU</Text>
      <Text style={styles.sectionValue}>{formatPercent(telemetry.cpu.utilizationPercent)}</Text>
      <AreaChart points={series} {...window} height={40} color={colors.accent} accessibilityLabel="CPU utilization history, scale 0–100%" />
      <PressureStrip spans={pressureSpans(telemetry)} {...window} />
      <Text style={styles.caption}>{cores.length ? `${cores.length} cores · peak ${formatPercent(Math.max(...cores))}` : "Core data unavailable"}</Text>
      {details ? <>
        {cores.length > 0 ? <CoreBars values={cores} /> : null}
        <Text style={styles.caption}>Load · 1 / 5 / 15 min</Text>
        <Text style={styles.mono}>{[telemetry.cpu.load1, telemetry.cpu.load5, telemetry.cpu.load15].map((v) => v?.toFixed(2) ?? "—").join(" / ")}</Text>
      </> : null}
    </View>
  );
}

export function MemorySection({ telemetry, styles, details }: SectionProps) {
  const { colors } = useAppTheme();
  const used = memoryUsedRatio(telemetry);
  const { totalBytes, availableBytes, cacheBytes, sharedBytes, swapTotalBytes, swapUsedBytes } = telemetry.memory;
  const usedBytes = totalBytes !== undefined && availableBytes !== undefined ? Math.max(0, totalBytes - availableBytes) : telemetry.memory.usedBytes;
  const color = (used ?? 0) >= 0.9 ? colors.warning : colors.accent;
  return (
    <View style={styles.surface}>
      <Text style={styles.label}>Memory</Text>
      <Text style={styles.sectionValue}>{formatPercent(used === undefined ? undefined : used * 100)}</Text>
      <AreaChart points={memoryHistorySeries(telemetry)} {...chartWindow(telemetry)} height={40} color={color} accessibilityLabel="Memory utilization history, scale 0–100%" />
      <StackBar height={4} segments={[{ key: "used", ratio: used ?? 0, color }]} />
      <Text style={styles.caption}>{formatBytes(usedBytes)} / {formatBytes(totalBytes)}</Text>
      {details ? <>
        <Text style={styles.mono}>Available {formatBytes(availableBytes)}</Text>
        <Text style={styles.caption}>Cache {formatBytes(cacheBytes)} · Shared {formatBytes(sharedBytes)}</Text>
        <Text style={styles.caption}>Swap {formatBytes(swapUsedBytes)} / {formatBytes(swapTotalBytes)}</Text>
        {swapTotalBytes ? <StackBar height={4} segments={[{ key: "swap", ratio: (swapUsedBytes ?? 0) / swapTotalBytes, color: colors.warning }]} /> : null}
      </> : null}
    </View>
  );
}

export function Legend({ color, label, styles }: { color: string; label: string; styles: ResourceStyles }) {
  return <View style={styles.legendItem}>
    <View style={[styles.legendSwatch, { backgroundColor: color }]} />
    <Text style={styles.caption}>{label}</Text>
  </View>;
}
