import React, { useMemo } from "react";
import { Ionicons } from "@expo/vector-icons";
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography, TypeScale } from "../../constants/tokens";
import type { BrainObjective } from "../../store/brain";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import {
  BRAIN_WORK_GROUP_LABELS,
  BRAIN_WORK_GROUP_ORDER,
  brainWorkSummaryLine,
  type BrainWorkSlip,
  type BrainWorkSurface,
} from "./brainWorkSurface";
import { WorkSlip, WorkStatusWord } from "./WorkSlip";

/** Width of the Work column beside the conversation on wide screens. */
export const BRAIN_WORK_COLUMN_WIDTH = 360;

function slipMeta(slip: BrainWorkSlip): string | undefined {
  return [slip.who, formatChatBubbleTime(slip.updatedAt)].filter(Boolean).join(" · ") || undefined;
}

/**
 * Current Work as slips, one caption per state. The first needs-you slip
 * carries the cat when `perch` is set (the wide column is where it sits).
 */
export function BrainWorkList({
  surface,
  chrome,
  perch,
  animate,
  onOpenSlip,
}: {
  surface: BrainWorkSurface;
  chrome: TerminalThemeChrome;
  perch?: boolean;
  animate?: boolean;
  onOpenSlip(slip: BrainWorkSlip): void;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  if (surface.slips.length === 0) {
    return (
      <Text style={styles.empty}>
        Nothing out right now. Work Brain hands off shows up here.
      </Text>
    );
  }
  const firstNeeds = surface.slips.find((slip) => slip.group === "needs")?.workId;
  return (
    <View>
      {BRAIN_WORK_GROUP_ORDER.map((group) => {
        const slips = surface.slips.filter((slip) => slip.group === group);
        if (slips.length === 0) return null;
        return (
          <View key={group} style={styles.group}>
            <Text accessibilityRole="header" style={styles.groupLabel}>
              {BRAIN_WORK_GROUP_LABELS[group]} · {slips.length}
            </Text>
            {slips.map((slip) => (
              <WorkSlip
                key={slip.workId}
                chrome={chrome}
                status={slip.status}
                statusLabel={slip.statusLabel}
                title={slip.title}
                meta={slipMeta(slip)}
                summary={slip.summary}
                unread={slip.unread}
                perched={Boolean(perch && slip.workId === firstNeeds)}
                animate={animate}
                onPress={() => onOpenSlip(slip)}
                accessibilityLabel={[slip.statusLabel, slip.title, slip.summary].filter(Boolean).join(", ")}
              />
            ))}
          </View>
        );
      })}
    </View>
  );
}

/** Wide screens: the Work column to the right of the conversation. */
export function BrainWorkColumn({
  surface,
  chrome,
  topInset,
  animate,
  onOpenSlip,
}: {
  surface: BrainWorkSurface;
  chrome: TerminalThemeChrome;
  topInset: number;
  animate: boolean;
  onOpenSlip(slip: BrainWorkSlip): void;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  return (
    <View
      role="complementary"
      accessibilityLabel="Work"
      style={[styles.column, { paddingTop: topInset }]}
    >
      <View style={styles.columnHeader}>
        <Text accessibilityRole="header" style={styles.columnTitle}>Work</Text>
        <Text numberOfLines={1} style={styles.columnSummary}>
          {brainWorkSummaryLine(surface.counts) ?? ""}
        </Text>
      </View>
      <ScrollView contentContainerStyle={styles.columnContent}>
        <BrainWorkList
          surface={surface}
          chrome={chrome}
          perch
          animate={animate}
          onOpenSlip={onOpenSlip}
        />
      </ScrollView>
    </View>
  );
}

/**
 * Under the title: Brain's objective ("… · 2 of 5 back") and, on the phone,
 * the one Work line ("1 needs you · 3 running") that opens the Work list.
 */
export function BrainWorkHeader({
  objective,
  surface,
  chrome,
  showSummary,
  onOpenWork,
}: {
  objective?: BrainObjective;
  surface: BrainWorkSurface;
  chrome: TerminalThemeChrome;
  showSummary: boolean;
  onOpenWork(): void;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const summary = showSummary ? brainWorkSummaryLine(surface.counts) : null;
  if (!objective && !summary) return null;
  return (
    <View style={styles.header}>
      {objective ? <BrainGoalLine objective={objective} chrome={chrome} /> : null}
      {summary ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={`Work: ${summary}`}
          accessibilityHint="Opens the Work list"
          onPress={onOpenWork}
          hitSlop={6}
          style={({ pressed }) => [styles.summaryRow, pressed ? styles.pressed : null]}
        >
          {surface.counts.needs ? <View style={styles.sealDot} /> : null}
          <Text numberOfLines={1} style={styles.summaryText}>
            {summary}
          </Text>
          <Ionicons name="chevron-forward" size={14} color={chrome.textSubtle} />
        </Pressable>
      ) : null}
    </View>
  );
}

/** "Ship atlas-notes v1.4 this week · 2 of 5 back" */
export function BrainGoalLine({
  objective,
  chrome,
}: {
  objective: BrainObjective;
  chrome: TerminalThemeChrome;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  return (
    <Text
      numberOfLines={2}
      accessibilityLabel={`Objective: ${objective.title}, ${objective.back} of ${objective.total} back`}
      style={styles.goal}
    >
      {objective.title}
      {objective.total > 0 ? (
        <Text style={styles.goalProgress}>
          {" · "}
          {objective.back} of {objective.total} back
        </Text>
      ) : null}
    </Text>
  );
}

/** Phone: the Work list as a sheet. */
export function BrainWorkSheet({
  visible,
  surface,
  chrome,
  onClose,
  onOpenSlip,
}: {
  visible: boolean;
  surface: BrainWorkSurface;
  chrome: TerminalThemeChrome;
  onClose(): void;
  onOpenSlip(slip: BrainWorkSlip): void;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  return (
    <BottomSheetFrame visible={visible} onClose={onClose} maxHeight="85%" cardStyle={{ backgroundColor: chrome.appBackground }}>
      <View style={styles.sheetHeader}>
        <Text accessibilityRole="header" style={styles.columnTitle}>Work</Text>
        <Pressable accessibilityRole="button" accessibilityLabel="Close Work" onPress={onClose} style={styles.close}>
          <Ionicons name="close" size={22} color={chrome.textMuted} />
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.sheetContent}>
        <BrainWorkList surface={surface} chrome={chrome} onOpenSlip={onOpenSlip} />
      </ScrollView>
    </BottomSheetFrame>
  );
}

/** A Work with no live Session: what it is, where it stands, what it waits on. */
export function BrainWorkDetailSheet({
  slip,
  waitFor,
  chrome,
  onClose,
}: {
  slip: BrainWorkSlip | null;
  waitFor?: string;
  chrome: TerminalThemeChrome;
  onClose(): void;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const meta = slip ? slipMeta(slip) : undefined;
  return (
    <BottomSheetFrame visible={Boolean(slip)} onClose={onClose} cardStyle={{ backgroundColor: chrome.surface }}>
      {slip ? (
        <>
          <View style={styles.sheetHeader}>
            <Text selectable style={styles.detailTitle}>{slip.title}</Text>
            <Pressable accessibilityRole="button" accessibilityLabel="Close Work details" onPress={onClose} style={styles.close}>
              <Ionicons name="close" size={22} color={chrome.textMuted} />
            </Pressable>
          </View>
          <ScrollView contentContainerStyle={styles.detailContent}>
            <View style={styles.detailMeta}>
              <WorkStatusWord status={slip.status} label={slip.statusLabel} chrome={chrome} />
              {meta ? <Text style={styles.columnSummary}>{meta}</Text> : null}
            </View>
            {slip.summary ? <Text selectable style={styles.detailBody}>{slip.summary}</Text> : null}
            {waitFor && waitFor !== slip.summary ? (
              <Text selectable style={styles.columnSummary}>Waiting for: {waitFor}</Text>
            ) : null}
          </ScrollView>
        </>
      ) : null}
    </BottomSheetFrame>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    group: {
      marginBottom: 6,
    },
    groupLabel: {
      ...TypeScale.caption,
      fontFamily: Typography.uiFontMedium,
      color: chrome.textSubtle,
      marginBottom: 8,
      marginTop: 4,
    },
    empty: {
      ...TypeScale.compact,
      color: chrome.textMuted,
      paddingVertical: 12,
    },
    column: {
      width: BRAIN_WORK_COLUMN_WIDTH,
      borderLeftWidth: StyleSheet.hairlineWidth,
      borderLeftColor: chrome.border,
      backgroundColor: chrome.appBackground,
    },
    columnHeader: {
      paddingHorizontal: 20,
      paddingTop: 8,
      paddingBottom: 10,
      gap: 2,
    },
    columnTitle: {
      ...TypeScale.label,
      fontFamily: Typography.uiFontSemibold,
      color: chrome.text,
    },
    columnSummary: {
      ...TypeScale.caption,
      color: chrome.textMuted,
    },
    columnContent: {
      paddingHorizontal: 20,
      paddingBottom: 24,
    },
    header: {
      gap: 4,
      paddingBottom: 8,
    },
    goal: {
      ...TypeScale.compact,
      fontFamily: Typography.uiFontSemibold,
      color: chrome.text,
    },
    goalProgress: {
      fontFamily: Typography.uiFontMedium,
      color: chrome.textMuted,
    },
    summaryRow: {
      flexDirection: "row",
      alignItems: "center",
      alignSelf: "flex-start",
      gap: 6,
      minHeight: 28,
    },
    pressed: {
      opacity: 0.6,
    },
    sealDot: {
      width: 7,
      height: 7,
      borderRadius: 3.5,
      backgroundColor: chrome.seal,
    },
    summaryText: {
      ...TypeScale.compact,
      fontFamily: Typography.uiFontMedium,
      color: chrome.textMuted,
      flexShrink: 1,
    },
    sheetHeader: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    close: {
      width: 44,
      height: 44,
      marginLeft: "auto",
      alignItems: "center",
      justifyContent: "center",
    },
    sheetContent: {
      paddingTop: 4,
      paddingBottom: 12,
    },
    detailTitle: {
      ...TypeScale.body,
      fontFamily: Typography.uiFontSemibold,
      color: chrome.text,
      flex: 1,
    },
    detailContent: {
      gap: 12,
      paddingVertical: 8,
    },
    detailMeta: {
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
      flexWrap: "wrap",
    },
    detailBody: {
      ...TypeScale.compact,
      color: chrome.text,
      lineHeight: 20,
    },
  });
}
