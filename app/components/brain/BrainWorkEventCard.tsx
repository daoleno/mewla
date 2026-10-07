import React, { useMemo } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography, TypeScale } from "../../constants/tokens";
import { useBrainCompanion } from "../mewla/BrainCompanion";
import { SealCat } from "../mewla/SealCat";
import { StatusMark } from "../ui/StatusMark";
import { workStatusInk, workStatusTextInk, type WorkStatus } from "../ui/workStatus";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import { brainWorkEventCardModel } from "./brainWorkEventCardModel";
import {
  BRAIN_WORK_CARD_FACT_LINES,
  BRAIN_WORK_CARD_GAP,
  BRAIN_WORK_CARD_HORIZONTAL_PADDING,
  BRAIN_WORK_CARD_MIN_TITLE_WIDTH,
  BRAIN_WORK_CARD_SUMMARY_LINES,
  BRAIN_WORK_CARD_TITLE_LINES,
} from "./brainWorkEventCardLayout";
import {
  brainWorkEventAccessibilityLabel,
  brainCurrentWorkLifecycle,
  brainWorkEventLifecycle,
  brainWorkLifecycleStatus,
  brainWorkEventWorkTitle,
} from "./brainWorkEventPresentation";

export type BrainWorkEventTimelineItem = {
  type: "brain-work-event";
  id: string;
  timestamp: string;
  event: BrainWorkResultEvent;
  events: BrainWorkResultEvent[];
  sourceCount?: number;
  currentWork?: import("../../store/brain").BrainCurrentWork;
  onPress?: () => void;
  /** Brain only: the cat sits on this slip because its Work needs you. */
  catPerched?: boolean;
};

/** Room above a slip for the perched cat (the cat is ~44 pt tall). */
const PERCH_ROOM = 46;
const PERCH_CAT = 58;

/**
 * One Work slip in the Brain conversation: a plain card on the paper, the
 * state as a small glyph and word, the title first. Needs you is the one loud
 * slip: ink outline, the seal pill, and (when Brain is waiting on it) the cat
 * sitting on top. Blocked Work is a dashed, unfilled slip.
 */
export function BrainWorkEventCard({
  item,
  chrome,
}: {
  item: BrainWorkEventTimelineItem;
  chrome: TerminalThemeChrome;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const companion = useBrainCompanion();
  const presentation = item.currentWork
    ? brainCurrentWorkLifecycle(item.currentWork, item.event)
    : brainWorkEventLifecycle(item.event);
  const status = brainWorkLifecycleStatus(presentation.lifecycle);
  const needs = status === "needs";
  const workTitle = brainWorkEventWorkTitle(item.event);
  const card = brainWorkEventCardModel(item.event);
  const time = formatChatBubbleTime(item.event.occurred_at);
  const accessibilityTime = new Date(item.event.occurred_at).toLocaleString();
  const accessibilityLabel = brainWorkEventAccessibilityLabel({
    event: item.event,
    statusLabel: presentation.label,
    occurredAtLabel: accessibilityTime,
    description: [card.summary, ...card.facts].filter(
      (value): value is string => Boolean(value),
    ),
  });
  const perched = Boolean(item.catPerched && companion);
  const slipStyle = ({ pressed }: { pressed: boolean }) => [
    styles.slip,
    needs ? styles.slipNeeds : null,
    status === "blocked" ? styles.slipBlocked : null,
    pressed ? styles.slipPressed : null,
  ];
  const statusMark = (
    <StatusWord status={status} label={presentation.label} chrome={chrome} styles={styles} />
  );

  const slip =
    card.density === "minimal" ? (
      <Pressable
        accessibilityRole={item.onPress ? "button" : undefined}
        accessibilityLabel={accessibilityLabel}
        disabled={!item.onPress}
        onPress={item.onPress}
        style={(state) => [...slipStyle(state), styles.slipCompact]}
      >
        <Text
          numberOfLines={BRAIN_WORK_CARD_TITLE_LINES}
          style={styles.compactTitle}
        >
          {workTitle}
        </Text>
        {statusMark}
        <Text style={styles.time}>{time}</Text>
      </Pressable>
    ) : (
      <Pressable
        accessibilityRole={item.onPress ? "button" : undefined}
        accessibilityLabel={accessibilityLabel}
        disabled={!item.onPress}
        onPress={item.onPress}
        style={slipStyle}
      >
        <View style={styles.meta}>
          <Text numberOfLines={1} style={styles.who}>
            {item.event.session_name || "Work"} · {time}
          </Text>
          {item.event.unread ? <View accessibilityElementsHidden style={styles.unreadDot} /> : null}
          {statusMark}
        </View>
        <Text
          numberOfLines={BRAIN_WORK_CARD_TITLE_LINES}
          style={styles.title}
        >
          {workTitle}
        </Text>
        {card.summary ? (
          <Text numberOfLines={BRAIN_WORK_CARD_SUMMARY_LINES} style={styles.summary}>
            {card.summary}
          </Text>
        ) : null}
        {card.facts.length > 0 ? (
          <View style={styles.facts}>
            {card.facts.map((fact) => (
              <Text key={fact} numberOfLines={BRAIN_WORK_CARD_FACT_LINES} style={styles.fact}>
                {fact}
              </Text>
            ))}
          </View>
        ) : null}
      </Pressable>
    );

  if (!perched || !companion) return <View style={styles.wrap}>{slip}</View>;
  return (
    <View style={[styles.wrap, styles.wrapPerched]}>
      <SealCat
        state="attention"
        size={PERCH_CAT}
        animate={companion.animate}
        style={styles.perch}
      />
      {slip}
    </View>
  );
}

function StatusWord({
  status,
  label,
  chrome,
  styles,
}: {
  status: WorkStatus;
  label: string;
  chrome: TerminalThemeChrome;
  styles: ReturnType<typeof createStyles>;
}) {
  if (status === "needs") {
    return (
      <View style={styles.needsPill}>
        <Text style={styles.needsPillText}>{label}</Text>
      </View>
    );
  }
  const inks = chromeStatusInks(chrome);
  return (
    <View style={styles.status}>
      <StatusMark status={status} color={workStatusInk(status, inks)} knockout={chrome.surface} />
      <Text numberOfLines={1} style={[styles.statusLabel, { color: workStatusTextInk(status, inks) }]}>
        {label}
      </Text>
    </View>
  );
}

export function chromeStatusInks(chrome: TerminalThemeChrome) {
  return {
    statusReady: chrome.statusReady,
    statusRunning: chrome.statusRunning,
    seal: chrome.seal,
    sealText: chrome.sealText,
    statusWarning: chrome.statusWarning,
    statusFailed: chrome.danger,
    statusBlocked: chrome.statusBlocked,
  };
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    wrap: {
      marginHorizontal: 1,
      marginBottom: 10,
    },
    wrapPerched: {
      paddingTop: PERCH_ROOM,
    },
    perch: {
      position: "absolute",
      right: 18,
      top: 2,
    },
    slip: {
      paddingHorizontal: BRAIN_WORK_CARD_HORIZONTAL_PADDING + 2,
      paddingVertical: 14,
      borderRadius: 14,
      borderWidth: 1,
      borderColor: chrome.border,
      backgroundColor: chrome.surface,
    },
    slipNeeds: {
      borderWidth: 1.5,
      borderColor: chrome.text,
    },
    slipBlocked: {
      borderStyle: "dashed",
      backgroundColor: "transparent",
    },
    slipPressed: {
      opacity: 0.72,
    },
    slipCompact: {
      minHeight: 48,
      paddingVertical: 11,
      flexDirection: "row",
      alignItems: "center",
      gap: BRAIN_WORK_CARD_GAP + 3,
    },
    compactTitle: {
      ...TypeScale.compact,
      fontFamily: Typography.uiFontMedium,
      color: chrome.text,
      flex: 1,
      minWidth: BRAIN_WORK_CARD_MIN_TITLE_WIDTH,
    },
    meta: {
      flexDirection: "row",
      alignItems: "center",
      gap: BRAIN_WORK_CARD_GAP,
      marginBottom: 6,
    },
    who: {
      ...TypeScale.caption,
      color: chrome.textMuted,
      flex: 1,
      minWidth: 0,
    },
    status: {
      flexDirection: "row",
      alignItems: "center",
      gap: 5,
      flexShrink: 0,
    },
    statusLabel: {
      ...TypeScale.caption,
      fontFamily: Typography.uiFontMedium,
    },
    needsPill: {
      minHeight: 22,
      paddingHorizontal: 9,
      borderRadius: 11,
      justifyContent: "center",
      backgroundColor: chrome.seal,
      flexShrink: 0,
    },
    needsPillText: {
      ...TypeScale.caption,
      fontFamily: Typography.uiFontSemibold,
      color: chrome.onSeal,
    },
    title: {
      ...TypeScale.body,
      fontFamily: Typography.uiFontSemibold,
      color: chrome.text,
    },
    unreadDot: {
      width: 6,
      height: 6,
      borderRadius: 3,
      backgroundColor: chrome.text,
    },
    summary: {
      ...TypeScale.compact,
      color: chrome.textMuted,
      marginTop: 3,
      lineHeight: 20,
    },
    facts: {
      gap: 3,
      marginTop: 8,
    },
    fact: {
      ...TypeScale.caption,
      color: chrome.textMuted,
    },
    time: {
      ...TypeScale.caption,
      color: chrome.textSubtle,
    },
  });
}
