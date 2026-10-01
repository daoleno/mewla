import React, { useMemo } from "react";
import { Text, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import {
  chartWindow,
  clampRatio,
  diskThroughputSeries,
  formatBytes,
  formatPercent,
  formatRate,
  latestDiskThroughput,
  psiHistorySeries,
  psiLines,
  psiTrend,
  type PsiLine,
  type PsiTone,
  type ResourceTelemetry,
} from "../../services/resourceTelemetry";
import { AreaChart, StackBar } from "./ResourceCharts";
import { Legend } from "./ResourceOverviewSections";
import type { ResourceStyles } from "./resourceStyles";

interface SectionProps {
  telemetry: ResourceTelemetry;
  styles: ResourceStyles;
}

const TONE_LABEL: Record<PsiTone, string> = {
  calm: "Calm",
  some: "Some waiting",
  heavy: "Heavy waiting",
};

const TREND_LABEL = {
  rising: "rising",
  falling: "easing",
  steady: "steady",
} as const;

export function PressureSection({ telemetry, styles }: SectionProps) {
  const lines = useMemo(() => psiLines(telemetry), [telemetry]);
  if (lines.length === 0) return null;
  return (
    <View style={styles.section}>
      <Text style={styles.sectionTitle} accessibilityRole="header">Waiting</Text>
      <Text style={styles.sectionNote}>
        How often work had to wait for the CPU, memory or disk. Low is good. Waiting is what makes the
        machine feel slow, even when usage looks fine.
      </Text>
      <View style={{ gap: 16 }}>
        {lines.map((line) => (
          <PressureRow key={line.key} line={line} telemetry={telemetry} styles={styles} />
        ))}
      </View>
    </View>
  );
}

function PressureRow({ line, telemetry, styles }: { line: PsiLine; telemetry: ResourceTelemetry; styles: ResourceStyles }) {
  const { colors } = useAppTheme();
  const series = useMemo(() => psiHistorySeries(telemetry, line.key), [line.key, telemetry]);
  const { start, end } = chartWindow(telemetry);
  const tone = {
    calm: { fill: colors.surfaceSubtle, ink: colors.textSecondary },
    some: { fill: colors.warningSoft, ink: colors.warning },
    heavy: { fill: colors.dangerSoft, ink: colors.dangerText },
  }[line.tone];
  // Stall share rarely exceeds a few percent; scale charts to 25% so calm
  // stays a flat line and real waiting is visible.
  const scaled = useMemo(
    () => series.map((point) => ({ at: point.at, value: clampRatio(point.value * 4) })),
    [series],
  );
  return (
    <View
      style={{ gap: 6 }}
      accessible
      accessibilityLabel={`${line.label}, ${TONE_LABEL[line.tone]}. ${line.sentence}`}
    >
      <View style={styles.sectionHeader}>
        <Text style={styles.label}>{line.label}</Text>
        <View style={[styles.chip, { backgroundColor: tone.fill }]}>
          <Text style={[styles.micro, { color: tone.ink }]}>{TONE_LABEL[line.tone]}</Text>
        </View>
      </View>
      <Text style={styles.sectionNote}>{line.sentence}</Text>
      {scaled.length > 1 ? (
        <AreaChart
          points={scaled}
          start={start}
          end={end}
          height={32}
          color={line.tone === "calm" ? colors.accent : tone.ink}
          accessibilityLabel={`${line.label} waiting over time`}
        />
      ) : null}
      <Text style={styles.caption}>
        1 min {formatPercent(line.avg60)} · 5 min {formatPercent(line.avg300)} · {TREND_LABEL[psiTrend(line)]}
      </Text>
    </View>
  );
}

export function DiskSection({ telemetry, styles }: SectionProps) {
  const { colors } = useAppTheme();
  const throughput = useMemo(() => diskThroughputSeries(telemetry), [telemetry]);
  const latest = latestDiskThroughput(telemetry);
  const { start, end } = chartWindow(telemetry);
  if (telemetry.disks.length === 0 && throughput.read.length === 0) return null;
  return (
    <View style={styles.section}>
      <Text style={styles.sectionTitle} accessibilityRole="header">Disk</Text>
      {telemetry.disks.map((disk) => {
        const ratio = disk.totalBytes > 0 ? disk.usedBytes / disk.totalBytes : 0;
        const low = disk.totalBytes > 0 && disk.freeBytes / disk.totalBytes < 0.1;
        return (
          <View
            key={disk.mount}
            style={{ gap: 6 }}
            accessible
            accessibilityLabel={`${disk.mount}, ${formatBytes(disk.freeBytes)} free of ${formatBytes(disk.totalBytes)}`}
          >
            <View style={styles.sectionHeader}>
              <Text style={styles.monoStrong} numberOfLines={1}>{disk.mount}</Text>
              <Text style={[styles.mono, low && { color: colors.warning }]}>
                {formatBytes(disk.freeBytes)} free of {formatBytes(disk.totalBytes)}
              </Text>
            </View>
            <StackBar height={6} segments={[{ key: "used", ratio, color: low ? colors.warning : colors.accent, opacity: 0.75 }]} />
          </View>
        );
      })}
      {throughput.read.length > 1 || throughput.write.length > 1 ? (
        <View style={{ gap: 6 }}>
          <View>
            <AreaChart
              points={throughput.read}
              start={start}
              end={end}
              height={48}
              color={colors.accent}
              accessibilityLabel={`Disk reads over time, now ${formatRate(latest.read)}`}
            />
            <View style={{ position: "absolute", left: 0, right: 0, top: 0 }}>
              <AreaChart
                points={throughput.write}
                start={start}
                end={end}
                height={48}
                color={colors.statusUnknown}
                grid={false}
                accessibilityLabel={`Disk writes over time, now ${formatRate(latest.write)}`}
              />
            </View>
          </View>
          <View style={styles.sectionHeader}>
            <View style={styles.legendRow}>
              <Legend color={colors.accent} label={`Read ${formatRate(latest.read)}`} styles={styles} />
              <Legend color={colors.statusUnknown} label={`Write ${formatRate(latest.write)}`} styles={styles} />
            </View>
            <Text style={styles.micro}>peak {formatRate(throughput.peak)}</Text>
          </View>
        </View>
      ) : latest.read !== undefined || latest.write !== undefined ? (
        <View style={styles.legendRow}>
          <Legend color={colors.accent} label={`Read ${formatRate(latest.read)}`} styles={styles} />
          <Legend color={colors.statusUnknown} label={`Write ${formatRate(latest.write)}`} styles={styles} />
        </View>
      ) : null}
    </View>
  );
}
