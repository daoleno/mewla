import React from "react";
import { Text, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import {
  chartWindow, diskThroughputSeries, formatBytes, formatPressurePercent, formatRate,
  latestDiskThroughput, psiHistorySeries,
} from "../../services/resourceTelemetry";
import { AreaChart, StackBar, pressureInk } from "./ResourceCharts";
import { Legend, type SectionProps } from "./ResourceOverviewSections";

export function PressureSection({ telemetry, styles, details }: SectionProps) {
  const { colors } = useAppTheme();
  const values = (["cpu", "memory", "io"] as const).map((key) => ({ key, value: telemetry.psi[key]?.some?.avg10 }));
  const peak = values.reduce<typeof values[number] | undefined>((best, next) =>
    next.value !== undefined && (best?.value === undefined || next.value > best.value) ? next : best, undefined);
  const labels = { cpu: "CPU", memory: "Mem", io: "I/O" };
  // Pressure charts are ink until the machine is actually under pressure.
  const ink = pressureInk(telemetry.state, colors);
  return (
    <View style={styles.surface}>
      <View style={styles.tileHead}>
        <Text style={styles.label}>Pressure</Text>
        <Text style={styles.sectionValue}>{formatPressurePercent(peak?.value)}</Text>
      </View>
      <AreaChart points={peak ? psiHistorySeries(telemetry, peak.key) : []} {...chartWindow(telemetry)} height={36}
        color={ink} accessibilityLabel={`PSI ${peak ? labels[peak.key] : "unavailable"} some history, scale 0–100%`} />
      <Text style={styles.caption}>{peak ? `${labels[peak.key]} · max some 10s` : "PSI unavailable"}</Text>
      <Text style={styles.micro}>{values.map(({ key, value }) => `${labels[key]} ${formatPressurePercent(value)}`).join(" · ")}</Text>
      {details ? <>
        <Text style={styles.caption}>some / full · 10s / 1m / 5m</Text>
        {values.map(({ key }) => <View key={key} style={{ gap: 2 }}>
          <Text style={styles.label}>{labels[key]}</Text>
          {(["some", "full"] as const).map((kind) => {
            const averages = telemetry.psi[key]?.[kind];
            return <Text key={kind} style={styles.micro}>{kind} {(["avg10", "avg60", "avg300"] as const).map((v) => formatPressurePercent(averages?.[v])).join(" / ")}</Text>;
          })}
          <AreaChart points={psiHistorySeries(telemetry, key)} {...chartWindow(telemetry)} height={24}
            color={ink} accessibilityLabel={`${labels[key]} PSI some history, scale 0–100%`} />
        </View>)}
      </> : null}
    </View>
  );
}

export function DiskSection({ telemetry, styles, details }: SectionProps) {
  const { colors } = useAppTheme();
  const throughput = diskThroughputSeries(telemetry);
  const latest = latestDiskThroughput(telemetry);
  return (
    <View style={styles.surface}>
      <View style={styles.tileHead}>
        <Text style={styles.label}>Disk I/O</Text>
        <Text style={styles.sectionValue}>{formatRate(latest.read === undefined && latest.write === undefined ? undefined : (latest.read ?? 0) + (latest.write ?? 0))}</Text>
      </View>
      <View>
        <AreaChart points={throughput.read} {...chartWindow(telemetry)} height={36} color={colors.accent} accessibilityLabel="Disk read throughput history" />
        <View pointerEvents="none" style={{ position: "absolute", left: 0, right: 0, top: 0 }}>
          <AreaChart points={throughput.write} {...chartWindow(telemetry)} height={36} color={colors.statusUnknown} grid={false} accessibilityLabel="Disk write throughput history" />
        </View>
      </View>
      <Legend color={colors.accent} label={`R ${formatRate(latest.read)}`} styles={styles} />
      <Legend color={colors.statusUnknown} label={`W ${formatRate(latest.write)}`} styles={styles} />
      {details ? <>
        <Text style={styles.caption}>Chart scale {formatRate(throughput.peak)}</Text>
        {telemetry.disks.length === 0 ? <Text style={styles.caption}>Mount data unavailable</Text> : null}
        {telemetry.disks.map((disk) => <View key={disk.mount} style={{ gap: 4 }}>
          <Text style={styles.monoStrong}>{disk.mount}</Text>
          <Text style={styles.caption}>{formatBytes(disk.freeBytes)} free / {formatBytes(disk.totalBytes)}</Text>
          <StackBar height={4} segments={[{ key: "used", ratio: disk.totalBytes ? disk.usedBytes / disk.totalBytes : 0,
            color: disk.totalBytes && disk.freeBytes / disk.totalBytes < 0.1 ? colors.warning : colors.accent }]} />
          <Text style={styles.micro}>R {formatRate(disk.readBytesPerSecond)} · W {formatRate(disk.writeBytesPerSecond)}</Text>
        </View>)}
      </> : null}
    </View>
  );
}
