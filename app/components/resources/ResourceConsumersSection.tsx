import React, { useMemo, useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { TypeScale, UiTextMetrics, useAppTheme } from "../../constants/tokens";
import {
  consumerMeta,
  consumerTitle,
  formatBytes,
  formatCores,
  groupConsumers,
  kindChips,
  type ConsumerGroup,
  type ResourceConsumer,
  type ResourceTelemetry,
} from "../../services/resourceTelemetry";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { StackBar } from "./ResourceCharts";
import type { ResourceStyles } from "./resourceStyles";

const VISIBLE_PER_GROUP = 4;

export function ConsumersSection({ telemetry, styles }: { telemetry: ResourceTelemetry; styles: ResourceStyles }) {
  const groups = useMemo(() => groupConsumers(telemetry.consumers), [telemetry.consumers]);
  if (groups.length === 0) return null;
  const total = telemetry.memory.totalBytes ?? Math.max(...telemetry.consumers.map((consumer) => consumer.rssBytes), 1);
  return (
    <View style={styles.section}>
      <Text style={styles.sectionTitle} accessibilityRole="header">Who&apos;s using it</Text>
      <View style={{ gap: 20 }}>
        {groups.map((group) => (
          <GroupBlock key={group.key} group={group} total={total} styles={styles} />
        ))}
      </View>
    </View>
  );
}

function GroupBlock({ group, total, styles }: { group: ConsumerGroup; total: number; styles: ResourceStyles }) {
  const { colors } = useAppTheme();
  const [expanded, setExpanded] = useState(false);
  const visible = expanded ? group.consumers : group.consumers.slice(0, VISIBLE_PER_GROUP);
  const hidden = group.consumers.length - visible.length;
  const orphaned = group.key === "orphaned";
  return (
    <View style={{ gap: 2 }}>
      <View style={styles.sectionHeader}>
        <Text style={[styles.label, orphaned && { color: colors.warning }]}>{group.title}</Text>
        <Text style={styles.mono}>
          {formatBytes(group.rssBytes)} · {formatCores(group.cpuPercent)}
        </Text>
      </View>
      {group.hint ? <Text style={styles.caption}>{group.hint}</Text> : null}
      {visible.map((consumer, index) => (
        <ConsumerRow
          key={`${consumer.owner}:${consumer.workerId ?? consumer.workId ?? consumer.title ?? index}`}
          consumer={consumer}
          total={total}
          styles={styles}
        />
      ))}
      {hidden > 0 || expanded ? (
        <AnimatedPressable
          style={{ minHeight: 44, justifyContent: "center" }}
          scale={0.97}
          accessibilityRole="button"
          accessibilityState={{ expanded }}
          accessibilityLabel={expanded ? `Show fewer ${group.title}` : `Show ${hidden} more ${group.title}`}
          onPress={() => setExpanded((value) => !value)}
        >
          <Text style={[TypeScale.label, UiTextMetrics, { color: colors.accent }]}>
            {expanded ? "Show less" : `${hidden} more`}
          </Text>
        </AnimatedPressable>
      ) : null}
    </View>
  );
}

function ConsumerRow({ consumer, total, styles }: { consumer: ResourceConsumer; total: number; styles: ResourceStyles }) {
  const { colors } = useAppTheme();
  const chips = kindChips(consumer.kinds);
  const meta = consumerMeta(consumer);
  const share = total > 0 ? consumer.rssBytes / total : 0;
  const heavy = chips.some((chip) => chip.heavy);
  return (
    <View
      style={{
        paddingVertical: 10,
        gap: 6,
        borderBottomWidth: StyleSheet.hairlineWidth,
        borderBottomColor: colors.borderSubtle,
      }}
      accessible
      accessibilityLabel={[
        consumerTitle(consumer),
        meta,
        chips.map((chip) => chip.label).join(", "),
        `${formatBytes(consumer.rssBytes)} memory`,
        formatCores(consumer.cpuPercent),
      ].filter(Boolean).join(", ")}
    >
      <View style={[styles.sectionHeader, { alignItems: "flex-start" }]}>
        <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
          <Text style={[TypeScale.body, UiTextMetrics, { color: colors.textPrimary }]} numberOfLines={1}>
            {consumerTitle(consumer)}
          </Text>
          {meta ? <Text style={styles.caption} numberOfLines={1}>{meta}</Text> : null}
        </View>
        <View style={{ alignItems: "flex-end", gap: 2 }}>
          <Text style={styles.monoStrong}>{formatBytes(consumer.rssBytes)}</Text>
          <Text style={styles.caption}>{formatCores(consumer.cpuPercent)}</Text>
        </View>
      </View>
      <StackBar
        height={3}
        segments={[{ key: "rss", ratio: share, color: heavy ? colors.warning : colors.accent, opacity: 0.7 }]}
      />
      {chips.length > 0 ? (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 6 }}>
          {chips.map((chip) => (
            <View
              key={chip.label}
              style={[styles.chip, { backgroundColor: chip.heavy ? colors.warningSoft : colors.surfaceSubtle }]}
            >
              <Text style={[styles.micro, { color: chip.heavy ? colors.warning : colors.textSecondary }]}>
                {chip.label}
              </Text>
            </View>
          ))}
        </View>
      ) : null}
    </View>
  );
}
