import React, { useMemo, type ReactNode } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography, TypeScale } from "../../constants/tokens";
import { TappableCat } from "../mewla/TappableCat";
import { StatusMark } from "../ui/StatusMark";
import { workStatusInk, workStatusTextInk, type WorkStatus } from "../ui/workStatus";
import {
  BRAIN_WORK_CARD_GAP,
  BRAIN_WORK_CARD_HORIZONTAL_PADDING,
  BRAIN_WORK_CARD_SUMMARY_LINES,
  BRAIN_WORK_CARD_TITLE_LINES,
} from "./brainWorkEventCardLayout";

/** Room above a slip for the perched cat (the cat is ~44 pt tall). */
export const WORK_SLIP_PERCH_ROOM = 46;
const PERCH_CAT = 58;

/**
 * One Work slip: a plain card on the paper. The meta line says who and when,
 * the state is a small glyph and word, the title leads and one soft line says
 * what happened. Needs you is the one loud slip: ink outline, the seal pill,
 * and the cat on top when Brain is waiting on it. Blocked Work is a dashed,
 * unfilled slip. The Brain conversation and the Work column share it.
 */
export function WorkSlip({
  chrome,
  status,
  statusLabel,
  title,
  meta,
  summary,
  unread,
  perched,
  animate,
  onPress,
  accessibilityLabel,
  children,
  actions,
  onCatPress,
}: {
  chrome: TerminalThemeChrome;
  status: WorkStatus;
  statusLabel: string;
  title: string;
  /** "Claude Code · perpetuo · 20:11" */
  meta?: string;
  summary?: string;
  unread?: boolean;
  perched?: boolean;
  animate?: boolean;
  onPress?: () => void;
  accessibilityLabel: string;
  children?: ReactNode;
  /** Inline actions under the summary: the slip's next step, one tap away. */
  actions?: readonly WorkSlipAction[];
  /** Tapping the perched cat (it answers for Brain). */
  onCatPress?: () => void;
}) {
  const styles = useMemo(() => createWorkSlipStyles(chrome), [chrome]);
  const needs = status === "needs";
  const hasActions = Boolean(actions?.length);
  // The actions sit beside the tappable body, never inside it: a button in a
  // button is invalid on web and reads as one control to screen readers.
  const slip = (
    <View
      style={[
        styles.slip,
        needs ? styles.slipNeeds : null,
        status === "blocked" ? styles.slipBlocked : null,
      ]}
    >
      <Pressable
        accessibilityRole={onPress ? "button" : undefined}
        accessibilityLabel={accessibilityLabel}
        disabled={!onPress}
        onPress={onPress}
        style={({ pressed }) => [
          styles.slipBody,
          hasActions ? styles.slipBodyAboveActions : null,
          pressed ? styles.slipPressed : null,
        ]}
      >
        {/* With no who · when to show, the state sits beside the title. */}
        <View style={meta ? styles.meta : styles.titleRow}>
          {meta ? (
            <Text numberOfLines={1} style={styles.who}>
              {meta}
            </Text>
          ) : (
            <Text numberOfLines={BRAIN_WORK_CARD_TITLE_LINES} style={[styles.title, styles.titleInRow]}>
              {title}
            </Text>
          )}
          {unread && !needs ? <View accessibilityElementsHidden style={styles.unreadDot} /> : null}
          <WorkStatusWord status={status} label={statusLabel} chrome={chrome} />
        </View>
        {meta ? (
          <Text numberOfLines={BRAIN_WORK_CARD_TITLE_LINES} style={styles.title}>
            {title}
          </Text>
        ) : null}
        {summary ? (
          <Text numberOfLines={BRAIN_WORK_CARD_SUMMARY_LINES} style={styles.summary}>
            {summary}
          </Text>
        ) : null}
        {children}
      </Pressable>
      {actions?.length ? (
        <View style={styles.slipFoot}>
          <WorkSlipActions actions={actions} chrome={chrome} />
        </View>
      ) : null}
    </View>
  );
  if (!perched) return <View style={styles.wrap}>{slip}</View>;
  return (
    <View style={[styles.wrap, styles.wrapPerched]}>
      {slip}
      <TappableCat
        state="attention"
        size={PERCH_CAT}
        animate={animate}
        style={styles.perch}
        onPress={onCatPress}
        accessibilityLabel="Brain is waiting on this Work"
      />
    </View>
  );
}

export type WorkSlipAction = {
  key: string;
  label: string;
  primary?: boolean;
  busy?: boolean;
  disabled?: boolean;
  onPress(): void;
};

/** The mock's slip buttons: one ink primary, the rest outlined, compact. */
export function WorkSlipActions({
  actions,
  chrome,
}: {
  actions: readonly WorkSlipAction[];
  chrome: TerminalThemeChrome;
}) {
  const styles = useMemo(() => createWorkSlipStyles(chrome), [chrome]);
  return (
    <View style={styles.actions}>
      {actions.map((action) => (
        <Pressable
          key={action.key}
          accessibilityRole="button"
          accessibilityLabel={action.label}
          accessibilityState={{ busy: action.busy, disabled: action.disabled || action.busy }}
          disabled={action.disabled || action.busy}
          onPress={action.onPress}
          hitSlop={4}
          style={({ pressed }) => [
            styles.action,
            action.primary ? styles.actionPrimary : null,
            action.disabled ? styles.actionDisabled : null,
            pressed ? styles.slipPressed : null,
          ]}
        >
          <Text
            numberOfLines={1}
            style={[styles.actionLabel, action.primary ? styles.actionLabelPrimary : null]}
          >
            {action.busy ? "…" : action.label}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

/** The state as a glyph and word; Needs you is the filled seal pill. */
export function WorkStatusWord({
  status,
  label,
  chrome,
}: {
  status: WorkStatus;
  label: string;
  chrome: TerminalThemeChrome;
}) {
  const styles = useMemo(() => createWorkSlipStyles(chrome), [chrome]);
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

function createWorkSlipStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    wrap: {
      marginHorizontal: 1,
      marginBottom: 10,
    },
    wrapPerched: {
      paddingTop: WORK_SLIP_PERCH_ROOM,
    },
    perch: {
      position: "absolute",
      right: 18,
      top: 2,
    },
    slip: {
      borderRadius: 14,
      borderWidth: 1,
      borderColor: chrome.border,
      backgroundColor: chrome.surface,
    },
    slipBody: {
      paddingHorizontal: BRAIN_WORK_CARD_HORIZONTAL_PADDING + 2,
      paddingVertical: 14,
      borderRadius: 14,
    },
    slipBodyAboveActions: {
      paddingBottom: 0,
    },
    slipFoot: {
      paddingHorizontal: BRAIN_WORK_CARD_HORIZONTAL_PADDING + 2,
      paddingBottom: 14,
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
    meta: {
      flexDirection: "row",
      alignItems: "center",
      gap: BRAIN_WORK_CARD_GAP,
      marginBottom: 6,
    },
    titleRow: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: BRAIN_WORK_CARD_GAP,
    },
    titleInRow: {
      flex: 1,
      minWidth: 0,
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
    actions: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 8,
      marginTop: 12,
    },
    action: {
      minHeight: 32,
      paddingHorizontal: 13,
      borderRadius: 16,
      borderWidth: 1,
      borderColor: chrome.border,
      backgroundColor: chrome.surface,
      justifyContent: "center",
      maxWidth: "100%",
    },
    actionPrimary: {
      borderColor: chrome.text,
      backgroundColor: chrome.text,
    },
    actionDisabled: {
      opacity: 0.5,
    },
    actionLabel: {
      ...TypeScale.compact,
      fontFamily: Typography.uiFontMedium,
      color: chrome.text,
    },
    actionLabelPrimary: {
      color: chrome.surface,
    },
  });
}
