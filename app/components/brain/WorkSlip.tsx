import React, { useMemo, type ReactNode } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { Typography, TypeScale, useAppColors, type AppColors } from "../../constants/tokens";
import { TappableCat } from "../mewla/TappableCat";
import { Icon, type IconName } from "../icons/Icon";
import { Button } from "../ui/Button";
import { StatusPill, type StatusTone } from "../ui/StatusPill";
import type { WorkStatus } from "../ui/workStatus";
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
/** Reads its colours from the theme scope it sits in (chat chrome on Brain and Session screens). */
export function WorkSlip({
  status,
  statusLabel,
  statusShown = true,
  title,
  meta,
  metaIcon,
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
  status: WorkStatus;
  statusLabel: string;
  /** False when the list's group caption already says the same word. */
  statusShown?: boolean;
  title: string;
  /** "Claude Code · perpetuo · 20:11" */
  meta?: string;
  /** A glyph before the meta line: the calendar on a Calendar run's Work. */
  metaIcon?: IconName;
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
  const colors = useAppColors();
  const styles = useMemo(() => createWorkSlipStyles(colors), [colors]);
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
            <>
              {metaIcon ? <Icon name={metaIcon} size={12} color={colors.textSecondary} /> : null}
              <Text numberOfLines={1} style={styles.who}>
                {meta}
              </Text>
            </>
          ) : (
            <Text numberOfLines={BRAIN_WORK_CARD_TITLE_LINES} style={[styles.title, styles.titleInRow]}>
              {title}
            </Text>
          )}
          {unread && !needs ? <View accessibilityElementsHidden style={styles.unreadDot} /> : null}
          {statusShown ? <StatusPill tone={WORK_STATUS_TONE[status]} label={statusLabel} /> : null}
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
          <WorkSlipActions actions={actions} />
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

/** A Work state as the shared status pill's tone. */
export const WORK_STATUS_TONE: Readonly<Record<WorkStatus, StatusTone>> = {
  ready: "success",
  running: "accent",
  needs: "needs",
  warning: "warning",
  failed: "danger",
  blocked: "blocked",
};

/** The slip's buttons: one filled primary, the rest outlined, compact. */
export function WorkSlipActions({ actions }: { actions: readonly WorkSlipAction[] }) {
  return (
    <View style={actionStyles.actions}>
      {actions.map((action) => (
        <Button
          key={action.key}
          label={action.label}
          size="sm"
          variant={action.primary ? "filled" : "outlined"}
          loading={action.busy}
          disabled={action.disabled}
          onPress={action.onPress}
          hitSlop={4}
          style={actionStyles.action}
        />
      ))}
    </View>
  );
}

export function chromeStatusInks(chrome: {
  statusReady: string;
  statusRunning: string;
  seal: string;
  sealText: string;
  statusWarning: string;
  danger: string;
  statusBlocked: string;
}) {
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

function createWorkSlipStyles(colors: AppColors) {
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
      borderColor: colors.border,
      backgroundColor: colors.bgSurface,
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
      borderColor: colors.textPrimary,
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
      color: colors.textSecondary,
      flex: 1,
      minWidth: 0,
    },
    title: {
      ...TypeScale.body,
      fontFamily: Typography.uiFontSemibold,
      color: colors.textPrimary,
    },
    unreadDot: {
      width: 6,
      height: 6,
      borderRadius: 3,
      backgroundColor: colors.textPrimary,
    },
    summary: {
      ...TypeScale.compact,
      color: colors.textSecondary,
      marginTop: 3,
      lineHeight: 20,
    },
  });
}

const actionStyles = StyleSheet.create({
  actions: { flexDirection: "row", flexWrap: "wrap", gap: 8, marginTop: 12 },
  action: { maxWidth: "100%" },
});
