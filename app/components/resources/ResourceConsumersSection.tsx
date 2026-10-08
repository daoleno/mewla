import React, { useMemo, useState } from "react";
import { Text, View } from "react-native";
import { useAppTheme } from "../../constants/tokens";
import {
  CONSUMER_OWNER_LABEL, consumerKey, consumerMeta, consumerTitle, formatBytes, formatPercent,
  groupConsumers, kindChips, sortConsumers, type ConsumerSort, type ResourceConsumer,
  type ResourceConsumerOwner, type ResourceTelemetry,
} from "../../services/resourceTelemetry";
import { Icon, type IconName } from "../icons/Icon";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { IconButton } from "../ui/IconButton";
import type { ResourceStyles } from "./resourceStyles";

const PAGE = 30;

/** One glyph names the owner, so rows don't repeat "User process" in words. */
const OWNER_ICON: Record<ResourceConsumerOwner, IconName> = {
  worker: "terminal",
  orphaned_worker: "warning",
  brain: "brain",
  docker: "cube",
  user: "person",
};

const SORTS: readonly { value: ConsumerSort; icon: IconName; label: string }[] = [
  { value: "rss", icon: "memory", label: "Sort by memory" },
  { value: "cpu", icon: "chip", label: "Sort by CPU" },
];

/** What is using the machine: owners to filter by, then the heaviest consumers first. */
export function ConsumersSection({ telemetry, styles, wide = false }: { telemetry: ResourceTelemetry; styles: ResourceStyles; wide?: boolean }) {
  const { colors } = useAppTheme();
  const [sort, setSort] = useState<ConsumerSort>("rss");
  const [owner, setOwner] = useState<ResourceConsumerOwner | null>(null);
  const [limit, setLimit] = useState(PAGE);
  const groups = useMemo(() => groupConsumers(telemetry.consumers), [telemetry.consumers]);
  const consumers = sortConsumers(telemetry.consumers.filter((c) => owner === null || c.owner === owner), sort);
  // Malformed/legacy snapshots can contain duplicate identities. Keep every row.
  const keys = new Map<string, number>();
  const remaining = consumers.length - limit;
  return (
    <View style={styles.section}>
      <View style={styles.consumersHeader}>
        <Text style={[styles.sectionTitle, { flex: 1 }]} accessibilityRole="header">
          Using this machine <Text style={styles.caption}>{telemetry.consumers.length}</Text>
        </Text>
        {SORTS.map((option) => <IconButton key={option.value} icon={option.icon} size={36} iconSize={18}
          tone={sort === option.value ? "tinted" : "ghost"} accessibilityLabel={option.label}
          accessibilityState={{ selected: sort === option.value }} onPress={() => setSort(option.value)} />)}
      </View>
      <View style={styles.filterRow}>
        {[{ key: "all", title: "All", owner: null, count: telemetry.consumers.length },
          ...groups.map((group) => ({ key: group.key, title: group.title, owner: group.consumers[0].owner, count: group.consumers.length }))].map((group) => {
          const selected = owner === group.owner;
          const orphaned = group.owner === "orphaned_worker";
          const ink = orphaned ? colors.warning : selected ? colors.textPrimary : colors.textSecondary;
          return <AnimatedPressable key={group.key} accessibilityRole="button"
            accessibilityLabel={`Filter ${group.title}`} accessibilityState={{ selected }}
            onPress={() => { setOwner(group.owner); setLimit(PAGE); }}
            style={[styles.filter, { backgroundColor: selected ? colors.surfaceSubtle : "transparent",
              borderColor: selected ? colors.borderStrong : colors.borderSubtle }]}>
            {group.owner ? <Icon name={OWNER_ICON[group.owner]} size={14} color={ink} /> : null}
            <Text style={[styles.caption, { color: ink }]}>{group.title}</Text>
            <Text style={[styles.micro, { color: ink }]}>{group.count}</Text>
          </AnimatedPressable>;
        })}
      </View>
      <View style={styles.tableHeader}>
        <View style={{ flex: 1 }} />
        <Text style={[styles.micro, styles.cpuCell]}>CPU</Text>
        <Text style={[styles.micro, styles.rssCell]}>Memory</Text>
        {wide ? <Text style={[styles.micro, styles.countCell]}>Procs</Text> : null}
        <View style={styles.chevronCell} />
      </View>
      <View>
        {consumers.slice(0, limit).map((consumer) => {
          const key = consumerKey(consumer);
          const occurrence = keys.get(key) ?? 0;
          keys.set(key, occurrence + 1);
          return <ConsumerRow key={`${key}:${occurrence}`} consumer={consumer} styles={styles} wide={wide} />;
        })}
      </View>
      {consumers.length === 0 ? <Text style={styles.caption}>No consumers reported{owner ? " for this owner" : ""}.</Text> : null}
      {remaining > 0 ? <AnimatedPressable style={styles.moreButton} accessibilityRole="button"
        accessibilityLabel={`Show ${Math.min(PAGE, remaining)} more`} onPress={() => setLimit((v) => v + PAGE)}>
        <Icon name="chevron-down" size={16} color={colors.textSecondary} />
        <Text style={styles.caption}>{remaining} more</Text>
      </AnimatedPressable> : null}
      <Text style={styles.micro}>CPU 100% is one core. Memory is resident size and may count shared pages twice.</Text>
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
  // Status is news only for Workers; an unknown status says nothing.
  const status = orphaned ? `Residual · ${consumer.status ?? "unknown"}` : consumer.status && consumer.status !== "unknown" ? consumer.status : null;
  const meta = [status, consumerMeta(consumer)].filter(Boolean).join(" · ");
  return (
    <View style={styles.consumer}>
      <AnimatedPressable scale={1} accessibilityRole="button" accessibilityState={{ expanded }}
        accessibilityLabel={`${expanded ? "Collapse" : "Expand"} ${title}, ${CONSUMER_OWNER_LABEL[consumer.owner]}, ${consumer.status ?? "unknown"}, CPU ${formatPercent(consumer.cpuPercent)}, RSS ${formatBytes(consumer.rssBytes)}, ${consumer.processCount} processes`}
        onPress={() => setExpanded((v) => !v)} style={styles.consumerButton}>
        <View style={styles.consumerLine}>
          <Icon name={OWNER_ICON[consumer.owner]} size={16} color={orphaned ? colors.warning : colors.textTertiary} />
          <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
            <Text style={styles.label} numberOfLines={1}>{title}</Text>
            {meta ? <Text style={[styles.micro, orphaned && { color: colors.warning }]} numberOfLines={1}>{meta}</Text> : null}
          </View>
          <Text style={[styles.mono, styles.cpuCell]}>{formatPercent(consumer.cpuPercent)}</Text>
          <Text style={[styles.monoStrong, styles.rssCell]}>{formatBytes(consumer.rssBytes)}</Text>
          {wide ? <Text style={[styles.mono, styles.countCell]}>{consumer.processCount}</Text> : null}
          <View style={styles.chevronCell}>
            <Icon name={expanded ? "chevron-up" : "chevron-down"} size={16} color={colors.textTertiary} />
          </View>
        </View>
      </AnimatedPressable>
      {expanded ? <View style={styles.processPanel}>
        <Text style={styles.label} selectable>{title}</Text>
        <Text style={styles.caption}>{CONSUMER_OWNER_LABEL[consumer.owner]} · {consumer.processCount} processes</Text>
        {consumer.id ? <Text selectable style={styles.micro}>ID {consumer.id}</Text> : null}
        {consumer.workerId ? <Text selectable style={styles.micro}>Worker {consumer.workerId}</Text> : null}
        {consumer.workId ? <Text selectable style={styles.micro}>Work {consumer.workId}</Text> : null}
        {consumerMeta(consumer) ? <Text selectable style={styles.caption}>{consumerMeta(consumer)}</Text> : null}
        {commands.length ? <Text selectable style={styles.caption}>Commands · {commands.join(", ")}</Text> : null}
        {kinds ? <Text style={styles.caption}>Kinds · {kinds}</Text> : null}
        <Text style={styles.micro}>{consumer.processes.length} / {consumer.processCount} processes reported · largest memory</Text>
        <View style={styles.consumerLine}>
          <Text style={[styles.micro, styles.pidCell]}>PID</Text>
          <Text style={[styles.micro, { flex: 1 }]}>Command / start token</Text>
          <Text style={[styles.micro, styles.rssCell]}>Memory</Text>
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
