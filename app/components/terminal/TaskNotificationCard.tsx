import React, { useMemo } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography, TypeScale } from "../../constants/tokens";
import { chromeStatusInks } from "../brain/BrainWorkEventCard";
import { StatusMark } from "../ui/StatusMark";
import { workStatusInk, workStatusTextInk, type WorkStatus } from "../ui/workStatus";
import { InterfaceTimelineActivityExpandIcon } from "./InterfaceTimelineActivityExpandIcon";
import { useTimelineActivityExpansion } from "./InterfaceTimelineActivityExpansionState";
import { MessageBody } from "./InterfaceMessageBody";
import type {
  TaskNotificationTimelineItem,
  TaskNotificationTone,
} from "./taskNotificationCardModel";

/**
 * Background-task completion card. Shares the Brain Work card surface; the
 * result Markdown stays collapsed to a short excerpt until the user expands.
 */
export function TaskNotificationCard({
  item,
  chrome,
  theme,
}: {
  item: TaskNotificationTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const expandable = Boolean(item.body) || item.details.length > 0;
  const { expanded, toggle } = useTimelineActivityExpansion(item.id, false);
  const status = TONE_STATUS[item.tone];
  const inks = chromeStatusInks(chrome);
  const time = item.timestamp ? formatChatBubbleTime(item.timestamp) : "";
  const accessibilityLabel = [
    `${item.kindLabel} ${item.statusLabel}`,
    item.title,
    item.detail,
    item.facts.join(", "),
    expanded ? undefined : item.preview,
  ]
    .filter(Boolean)
    .join(". ");

  return (
    <Pressable
      accessibilityRole={expandable ? "button" : undefined}
      accessibilityLabel={accessibilityLabel}
      accessibilityState={expandable ? { expanded } : undefined}
      disabled={!expandable}
      onPress={toggle}
      style={({ pressed }) => [styles.wrap, pressed ? styles.wrapPressed : null]}
    >
      <View style={styles.header}>
        <Text numberOfLines={1} style={styles.kind}>
          {item.kindLabel}
          {time ? ` · ${time}` : ""}
        </Text>
        <View style={styles.status}>
          {status ? (
            <StatusMark status={status} color={workStatusInk(status, inks)} knockout={chrome.surface} />
          ) : null}
          <Text
            numberOfLines={1}
            style={[styles.statusLabel, { color: status ? workStatusTextInk(status, inks) : chrome.textMuted }]}
          >
            {item.statusLabel}
          </Text>
        </View>
      </View>

      <Text numberOfLines={2} style={styles.title}>
        {item.title}
      </Text>
      {item.detail ? (
        <Text numberOfLines={2} style={styles.detail}>
          {item.detail}
        </Text>
      ) : null}
      {item.facts.length > 0 ? (
        <View style={styles.facts}>
          {item.facts.map((fact, index) => (
            <React.Fragment key={fact}>
              {index > 0 ? (
                <View accessibilityElementsHidden style={styles.factDot} />
              ) : null}
              <Text style={styles.fact}>{fact}</Text>
            </React.Fragment>
          ))}
        </View>
      ) : null}

      {expanded ? (
        <View style={styles.expanded}>
          {item.body ? (
            <MessageBody value={item.body} chrome={chrome} theme={theme} compact />
          ) : null}
          {item.details.length > 0 ? (
            <View style={styles.details}>
              {item.details.map((detail) => (
                <Text key={detail.label} selectable numberOfLines={1} style={styles.detailId}>
                  {detail.label} {detail.value}
                </Text>
              ))}
            </View>
          ) : null}
        </View>
      ) : item.preview ? (
        <Text numberOfLines={2} style={styles.preview}>
          {item.preview}
        </Text>
      ) : null}

      <View style={styles.footer}>
        {expandable ? (
          <>
            <InterfaceTimelineActivityExpandIcon expanded={expanded} chrome={chrome} />
            <Text style={styles.toggleLabel}>
              {expanded ? "Hide result" : item.body ? "Show result" : "Details"}
            </Text>
          </>
        ) : null}
      </View>
    </Pressable>
  );
}

// Seal & Slip states: Done is the Ready check, Failed the crossed box,
// Stopped the inert dashed ring; a plain update carries no mark.
const TONE_STATUS: Record<TaskNotificationTone, WorkStatus | null> = {
  accent: "ready",
  danger: "failed",
  attention: "blocked",
  neutral: null,
};

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    // The same slip as Brain's Work cards: a plain card on the paper.
    wrap: {
      marginHorizontal: 1,
      marginBottom: 10,
      paddingHorizontal: 16,
      paddingVertical: 14,
      borderRadius: 14,
      borderWidth: 1,
      borderColor: chrome.border,
      backgroundColor: chrome.surface,
    },
    wrapPressed: {
      opacity: 0.72,
    },
    header: {
      flexDirection: "row",
      alignItems: "center",
      gap: 7,
      marginBottom: 6,
    },
    kind: {
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
    title: {
      ...TypeScale.body,
      fontFamily: Typography.uiFontSemibold,
      color: chrome.text,
    },
    detail: {
      ...TypeScale.compact,
      color: chrome.textMuted,
      marginTop: 3,
    },
    facts: {
      flexDirection: "row",
      flexWrap: "wrap",
      alignItems: "center",
      columnGap: 8,
      rowGap: 2,
      marginTop: 6,
    },
    factDot: {
      width: 3,
      height: 3,
      borderRadius: 2,
      backgroundColor: chrome.textSubtle,
    },
    fact: {
      ...TypeScale.caption,
      color: chrome.textMuted,
    },
    preview: {
      ...TypeScale.compact,
      color: chrome.text,
      marginTop: 8,
      lineHeight: 20,
    },
    expanded: {
      marginTop: 8,
      paddingTop: 8,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: chrome.border,
    },
    details: {
      gap: 2,
      marginTop: 8,
    },
    detailId: {
      ...TypeScale.caption,
      fontFamily: Typography.chatMonoFont,
      color: chrome.textSubtle,
    },
    footer: {
      flexDirection: "row",
      alignItems: "center",
      gap: 5,
      marginTop: 10,
      minHeight: 18,
    },
    toggleLabel: {
      ...TypeScale.caption,
      color: chrome.textSubtle,
    },
  });
}
