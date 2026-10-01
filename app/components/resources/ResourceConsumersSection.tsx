import React, { useMemo, useState } from "react";
import { Text, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import {
  CONSUMER_OWNER_LABEL, consumerKey, consumerMeta, consumerTitle, formatBytes, formatPercent,
  groupConsumers, kindChips, sortConsumers, type ConsumerSort, type ResourceConsumer,
  type ResourceConsumerOwner, type ResourceTelemetry,
} from "../../services/resourceTelemetry";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import type { ResourceStyles } from "./resourceStyles";

export function ConsumersSection({ telemetry, styles, wide = false }: { telemetry: ResourceTelemetry; styles: ResourceStyles; wide?: boolean }) {
  const { colors } = useAppTheme();
  const [sort, setSort] = useState<ConsumerSort>("rss");
  const [owner, setOwner] = useState<ResourceConsumerOwner | null>(null);
  const [limit, setLimit] = useState(30);
  const groups = useMemo(() => groupConsumers(telemetry.consumers), [telemetry.consumers]);
  const consumers = sortConsumers(telemetry.consumers.filter((c) => owner === null || c.owner === owner), sort);
  const totalRss = consumers.reduce((sum, c) => sum + c.rssBytes, 0);
  const totalCpu = consumers.every((c) => c.cpuPercent !== undefined)
    ? consumers.reduce((sum, c) => sum + c.cpuPercent!, 0) : undefined;
  // Malformed/legacy snapshots can contain duplicate identities. Keep every row.
  const keys = new Map<string, number>();
  return (
    <View style={styles.section}>
      <View style={styles.sectionHeader}>
        <View style={{ flex: 1 }}>
          <Text style={styles.sectionTitle} accessibilityRole="header">Consumers <Text style={styles.caption}>{telemetry.consumers.length}</Text></Text>
          <Text style={styles.micro}>CPU 100% = 1 core · RSS may share pages</Text>
        </View>
        <View style={styles.legendItem}>
          {(["rss", "cpu"] as const).map((value) => <AnimatedPressable key={value} accessibilityRole="button"
            accessibilityLabel={`Sort by ${value.toUpperCase()}`} accessibilityState={{ selected: sort === value }}
            onPress={() => setSort(value)} style={[styles.control, sort === value && { backgroundColor: colors.surfaceSubtle }]}>
            <Text style={[styles.label, sort === value && { color: colors.accent }]}>{value.toUpperCase()}{sort === value ? " ↓" : ""}</Text>
          </AnimatedPressable>)}
        </View>
      </View>
      <View style={styles.filterRow}>
        {[{ key: "all", title: "All", rssBytes: undefined, cpuPercent: undefined, consumers: telemetry.consumers }, ...groups].map((group) => {
          const value = group.key === "all" ? null : group.consumers[0].owner;
          return <AnimatedPressable key={group.key} accessibilityRole="button"
            accessibilityLabel={`Filter ${group.title}`} accessibilityState={{ selected: owner === value }}
            onPress={() => { setOwner(value); setLimit(30); }}
            style={[styles.filter, { backgroundColor: owner === value ? colors.surfaceSubtle : colors.bgPrimary,
              borderColor: owner === value ? colors.accent : colors.borderSubtle }]}>
            <Text style={[styles.caption, { color: value === "orphaned_worker" ? colors.warning : colors.textSecondary }]}>{group.title} · {group.consumers.length}</Text>
            {wide && group.rssBytes !== undefined ? <Text style={styles.micro}>{formatBytes(group.rssBytes)} · CPU {formatPercent(group.cpuPercent)}</Text> : null}
          </AnimatedPressable>;
        })}
      </View>
      {!wide ? <Text style={styles.micro}>{formatBytes(totalRss)} RSS · CPU {formatPercent(totalCpu)} · {consumers.reduce((sum, c) => sum + c.processCount, 0)} procs</Text> : null}
      {wide ? <View style={styles.tableHeader}>
        <Text style={[styles.micro, { flex: 1 }]}>NAME / EXECUTOR / CWD</Text>
        <Text style={[styles.micro, { width: 142 }]}>OWNER / STATUS</Text>
        <Text style={[styles.micro, styles.cpuCell]}>CPU</Text>
        <Text style={[styles.micro, styles.rssCell]}>RSS</Text>
        <Text style={[styles.micro, styles.countCell]}>PROCS</Text>
        <View style={{ width: 14 }} />
      </View> : null}
      <View>
        {consumers.slice(0, limit).map((consumer) => {
          const key = consumerKey(consumer);
          const occurrence = keys.get(key) ?? 0;
          keys.set(key, occurrence + 1);
          return <ConsumerRow key={`${key}:${occurrence}`} consumer={consumer} styles={styles} wide={wide} />;
        })}
      </View>
      {consumers.length === 0 ? <Text style={styles.caption}>No consumers reported{owner ? " for this owner" : ""}.</Text> : null}
      {consumers.length > limit ? <AnimatedPressable style={styles.control} accessibilityRole="button" onPress={() => setLimit((v) => v + 30)}>
        <Text style={styles.label}>Show more · {consumers.length - limit} remaining</Text>
      </AnimatedPressable> : null}
    </View>
  );
}

function ConsumerRow({ consumer, styles, wide }: { consumer: ResourceConsumer; styles: ResourceStyles; wide: boolean }) {
  const { colors } = useAppTheme();
  const [expanded, setExpanded] = useState(false);
  const title = consumerTitle(consumer);
  const orphaned = consumer.owner === "orphaned_worker";
  const commands = [...new Set([...consumer.commands, ...consumer.processes.flatMap((p) => p.command ? [p.command] : [])])];
  const kinds = kindChips(consumer.kinds).map((chip) => chip.label).join(" · ");
  const ownerStatus = <View style={wide ? { width: 142, gap: 2 } : styles.legendRow}>
    <Text style={[styles.caption, orphaned && { color: colors.warning }]}>{CONSUMER_OWNER_LABEL[consumer.owner]}</Text>
    <Text style={[styles.micro, orphaned && { color: colors.warning }]}>{orphaned ? `Residual · ${consumer.status ?? "unknown"}` : consumer.status ?? "unknown"}</Text>
  </View>;
  const metrics = <>
    <Text style={[styles.mono, wide && styles.cpuCell]}>{wide ? "" : "CPU "}{formatPercent(consumer.cpuPercent)}</Text>
    <Text style={[styles.monoStrong, wide && styles.rssCell]}>{wide ? "" : "RSS "}{formatBytes(consumer.rssBytes)}</Text>
    <Text style={[styles.mono, wide && styles.countCell]}>{consumer.processCount}{wide ? "" : " procs"}</Text>
  </>;
  return (
    <View style={styles.consumer}>
      <AnimatedPressable scale={1} accessibilityRole="button" accessibilityState={{ expanded }}
        accessibilityLabel={`${expanded ? "Collapse" : "Expand"} ${title}, ${CONSUMER_OWNER_LABEL[consumer.owner]}, ${consumer.status ?? "unknown"}, CPU ${formatPercent(consumer.cpuPercent)}, RSS ${formatBytes(consumer.rssBytes)}, ${consumer.processCount} processes`}
        onPress={() => setExpanded((v) => !v)} style={styles.consumerButton}>
        <View style={styles.consumerLine}>
          <View style={{ flex: 1, minWidth: 0, gap: 3 }}>
            <Text style={styles.label} numberOfLines={1}>{title}</Text>
            {consumerMeta(consumer) ? <Text style={styles.micro} numberOfLines={1}>{consumerMeta(consumer)}</Text> : null}
            {commands.length || kinds ? <Text style={styles.micro} numberOfLines={1}>{[commands.join(", "), kinds].filter(Boolean).join(" · ")}</Text> : null}
          </View>
          {wide ? <>{ownerStatus}{metrics}</> : null}
          <Text style={[styles.caption, { width: 14 }]}>{expanded ? "−" : "+"}</Text>
        </View>
        {!wide ? <>{ownerStatus}<View style={[styles.legendRow, { justifyContent: "space-between" }]}>{metrics}</View></> : null}
      </AnimatedPressable>
      {expanded ? <View style={styles.processPanel}>
        <Text style={styles.label} selectable>{title}</Text>
        {consumer.id ? <Text selectable style={styles.micro}>ID {consumer.id}</Text> : null}
        {consumer.workerId ? <Text selectable style={styles.micro}>Worker {consumer.workerId}</Text> : null}
        {consumer.workId ? <Text selectable style={styles.micro}>Work {consumer.workId}</Text> : null}
        {consumerMeta(consumer) ? <Text selectable style={styles.caption}>{consumerMeta(consumer)}</Text> : null}
        {commands.length ? <Text selectable style={styles.caption}>Commands · {commands.join(", ")}</Text> : null}
        {kinds ? <Text style={styles.caption}>Kinds · {kinds}</Text> : null}
        <Text style={styles.micro}>{consumer.processes.length} / {consumer.processCount} processes reported · largest RSS</Text>
        <View style={styles.consumerLine}>
          <Text style={[styles.micro, styles.pidCell]}>PID</Text>
          <Text style={[styles.micro, { flex: 1 }]}>COMMAND / START TOKEN</Text>
          <Text style={[styles.micro, styles.rssCell]}>RSS</Text>
        </View>
        {[...consumer.processes].sort((a, b) => (b.rssBytes ?? -1) - (a.rssBytes ?? -1)).map((process, index) => <View key={`${process.pid}:${process.start}:${index}`} style={styles.processLine}>
          <Text selectable style={[styles.mono, styles.pidCell]}>{process.pid ?? "—"}</Text>
          <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
            <Text selectable style={styles.monoStrong}>{process.command ?? "Unknown process"}</Text>
            {process.start ? <Text selectable style={styles.micro}>start {process.start}</Text> : null}
          </View>
          <Text style={[styles.mono, styles.rssCell]}>{formatBytes(process.rssBytes)}</Text>
        </View>)}
        {consumer.processes.length === 0 ? <Text style={styles.caption}>Process details unavailable in this sample.</Text> : null}
      </View> : null}
    </View>
  );
}
