import React, { useMemo } from "react";
import { Text, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import {
  PRESSURE_COPY,
  chartWindow,
  cpuHistorySeries,
  formatBytes,
  formatPercent,
  headlineSummary,
  historyWindowLabel,
  loadPerCore,
  memoryHistorySeries,
  memoryUsedRatio,
  pressureSpans,
  sampledAgoLabel,
  type ResourceTelemetry,
} from "../../services/resourceTelemetry";
import { AreaChart, CoreBars, PressureStrip, StackBar, pressureInk } from "./ResourceCharts";
import type { ResourceStyles } from "./resourceStyles";

interface SectionProps {
  telemetry: ResourceTelemetry;
  styles: ResourceStyles;
}

export function PressureHeadline({ telemetry, styles, now, statusLabel }: SectionProps & { now: number; statusLabel?: string }) {
  const { colors, theme } = useAppTheme();
  const copy = PRESSURE_COPY[telemetry.state];
  const ink = telemetry.state === "normal" ? colors.accentStrong : pressureInk(telemetry.state, colors);
  const fill = telemetry.state === "critical"
    ? colors.dangerSoft
    : telemetry.state === "elevated"
      ? colors.warningSoft
      : theme.materials.tint;
  const summary = headlineSummary(telemetry);
  return (
    <View
      style={styles.surface}
      accessible
      accessibilityRole="summary"
      accessibilityLabel={`${copy.title}. ${copy.detail} ${summary}`}
    >
      <View style={styles.sectionHeader}>
        <View style={[styles.chip, { backgroundColor: fill, flexDirection: "row", alignItems: "center", gap: 6 }]}>
          <View style={[styles.legendSwatch, { width: 6, height: 6, backgroundColor: ink }]} />
          <Text style={[styles.micro, { color: ink }]}>
            {telemetry.state === "normal" ? "Normal" : telemetry.state === "elevated" ? "Elevated" : "Critical"}
          </Text>
        </View>
        <Text style={styles.micro}>{statusLabel ?? sampledAgoLabel(telemetry.sampledAt, now)}</Text>
      </View>
      <Text style={[styles.sectionValue, { fontSize: 24, lineHeight: 32 }]} accessibilityRole="header">
        {copy.title}
      </Text>
      <Text style={styles.sectionNote}>{copy.detail}</Text>
      {summary ? <Text style={styles.mono}>{summary}</Text> : null}
    </View>
  );
}

function TimeAxis({ telemetry, styles }: SectionProps) {
  const label = historyWindowLabel(telemetry) ?? "Last few minutes";
  return (
    <View style={styles.axisRow}>
      <Text style={styles.micro}>{label}</Text>
      <Text style={styles.micro}>Now</Text>
    </View>
  );
}

export function CpuSection({ telemetry, styles }: SectionProps) {
  const { colors } = useAppTheme();
  const series = useMemo(() => cpuHistorySeries(telemetry), [telemetry]);
  const spans = useMemo(() => pressureSpans(telemetry), [telemetry]);
  const { start, end } = chartWindow(telemetry);
  const cores = telemetry.cpu.perCorePercent;
  const perCore = loadPerCore(telemetry.cpu.load15, cores.length);
  const busiest = cores.length > 0 ? Math.max(...cores) : undefined;
  const loads = [telemetry.cpu.load1, telemetry.cpu.load5, telemetry.cpu.load15];
  return (
    <View style={styles.section}>
      <View style={styles.sectionHeader}>
        <Text style={styles.sectionTitle} accessibilityRole="header">CPU</Text>
        <Text style={styles.sectionValue}>{formatPercent(telemetry.cpu.utilizationPercent)}</Text>
      </View>
      {series.length > 0 ? (
        <View>
          <AreaChart
            points={series}
            start={start}
            end={end}
            color={colors.accent}
            accessibilityLabel={`CPU use over time, now ${formatPercent(telemetry.cpu.utilizationPercent)}`}
          />
          <PressureStrip spans={spans} start={start} end={end} />
          <TimeAxis telemetry={telemetry} styles={styles} />
        </View>
      ) : null}
      {cores.length > 0 ? (
        <View style={{ gap: 6 }}>
          <CoreBars values={cores} />
          <Text style={styles.caption}>
            {cores.length} cores{busiest !== undefined ? ` · busiest ${formatPercent(busiest)}` : ""}
          </Text>
        </View>
      ) : null}
      {loads.some((value) => value !== undefined) ? (
        <View style={styles.factsRow}>
          {(["1 min", "5 min", "15 min"] as const).map((label, index) => (
            <View key={label} style={styles.fact}>
              <Text style={styles.micro}>Load · {label}</Text>
              <Text style={styles.monoStrong}>{loads[index] === undefined ? "—" : loads[index]!.toFixed(2)}</Text>
            </View>
          ))}
        </View>
      ) : null}
      {perCore !== undefined ? (
        <Text style={styles.sectionNote}>
          {perCore < 0.7
            ? "Cores have room to spare over the last 15 minutes."
            : perCore < 1
              ? "Cores have been mostly busy over the last 15 minutes."
              : "More work is queued than there are cores to run it."}
        </Text>
      ) : null}
    </View>
  );
}

export function MemorySection({ telemetry, styles }: SectionProps) {
  const { colors } = useAppTheme();
  const series = useMemo(() => memoryHistorySeries(telemetry), [telemetry]);
  const { start, end } = chartWindow(telemetry);
  const used = memoryUsedRatio(telemetry);
  const { totalBytes, availableBytes, cacheBytes, swapTotalBytes, swapUsedBytes } = telemetry.memory;
  const usedBytes = totalBytes !== undefined && availableBytes !== undefined
    ? totalBytes - availableBytes
    : telemetry.memory.usedBytes;
  const cacheRatio = totalBytes && cacheBytes !== undefined ? cacheBytes / totalBytes : 0;
  const usedRatio = used ?? 0;
  const color = usedRatio >= 0.9 ? colors.warning : colors.accent;
  const swapRatio = swapTotalBytes ? (swapUsedBytes ?? 0) / swapTotalBytes : undefined;
  return (
    <View style={styles.section}>
      <View style={styles.sectionHeader}>
        <Text style={styles.sectionTitle} accessibilityRole="header">Memory</Text>
        <Text style={styles.sectionValue}>{formatPercent(used === undefined ? undefined : used * 100)}</Text>
      </View>
      <Text style={styles.sectionNote}>
        {formatBytes(usedBytes)} of {formatBytes(totalBytes)} in use · {formatBytes(availableBytes)} available
      </Text>
      {series.length > 0 ? (
        <View>
          <AreaChart
            points={series}
            start={start}
            end={end}
            color={color}
            accessibilityLabel={`Memory use over time, now ${formatPercent(usedRatio * 100)}`}
          />
          <TimeAxis telemetry={telemetry} styles={styles} />
        </View>
      ) : null}
      <StackBar
        segments={[
          { key: "used", ratio: usedRatio, color },
          { key: "cache", ratio: Math.min(cacheRatio, 1 - usedRatio), color: colors.accent, opacity: 0.3 },
        ]}
      />
      <View style={styles.legendRow}>
        <Legend color={color} label={`In use ${formatBytes(usedBytes)}`} styles={styles} />
        {cacheBytes !== undefined ? (
          <Legend color={colors.accent} faded label={`Cache ${formatBytes(cacheBytes)}`} styles={styles} />
        ) : null}
        <Legend color={colors.borderSubtle} label={`Free ${formatBytes(availableBytes)}`} styles={styles} />
      </View>
      {swapRatio !== undefined ? (
        <View style={{ gap: 6 }}>
          <View style={styles.sectionHeader}>
            <Text style={styles.label}>Swap</Text>
            <Text style={styles.mono}>
              {formatBytes(swapUsedBytes)} of {formatBytes(swapTotalBytes)}
            </Text>
          </View>
          <StackBar
            height={6}
            segments={[{ key: "swap", ratio: swapRatio, color: swapRatio >= 0.5 ? colors.warning : colors.accent, opacity: 0.7 }]}
          />
          {swapRatio >= 0.25 ? (
            <Text style={styles.caption}>Swapping to disk makes everything slower.</Text>
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

export function Legend({ color, label, faded, styles }: { color: string; label: string; faded?: boolean; styles: ResourceStyles }) {
  return (
    <View style={styles.legendItem}>
      <View style={[styles.legendSwatch, { backgroundColor: color, opacity: faded ? 0.3 : 1 }]} />
      <Text style={styles.caption}>{label}</Text>
    </View>
  );
}
