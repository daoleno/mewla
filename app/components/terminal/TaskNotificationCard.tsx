import { Ionicons } from "@expo/vector-icons";
import React, { useMemo } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography, TypeScale } from "../../constants/tokens";
import { outlinedSurface } from "../ui/outlinedSurface";
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
  attentionColor,
  attentionBackground,
}: {
  item: TaskNotificationTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  attentionColor: string;
  attentionBackground: string;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const expandable = Boolean(item.body) || item.details.length > 0;
  const { expanded, toggle } = useTimelineActivityExpansion(item.id, false);
  const colors = toneColors(item.tone, chrome, attentionColor, attentionBackground);
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
        <View style={[styles.iconWrap, { backgroundColor: colors.background }]}>
          <Ionicons name={item.icon} size={15} color={colors.color} />
        </View>
        <Text numberOfLines={1} style={[styles.kind, { color: colors.color }]}>
          {item.kindLabel} · {item.statusLabel}
        </Text>
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
        {time ? <Text style={styles.time}>{time}</Text> : null}
      </View>
    </Pressable>
  );
}

function toneColors(
  tone: TaskNotificationTone,
  chrome: TerminalThemeChrome,
  attentionColor: string,
  attentionBackground: string,
) {
  switch (tone) {
    case "danger":
      return { color: chrome.danger, background: chrome.dangerSoft };
    case "attention":
      return { color: attentionColor, background: attentionBackground };
    case "accent":
      return { color: chrome.accent, background: chrome.accentSoft };
    default:
      return { color: chrome.textMuted, background: chrome.surfaceActive };
  }
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    wrap: {
      marginHorizontal: 1,
      marginBottom: 8,
      paddingHorizontal: 14,
      paddingVertical: 12,
      ...outlinedSurface(8),
      borderColor: chrome.border,
      // Chat chrome maps surfaceMuted to the user's sent bubble; a system card
      // must never read as the user's own message.
      backgroundColor: chrome.surface,
    },
    wrapPressed: {
      opacity: 0.72,
    },
    header: {
      flexDirection: "row",
      alignItems: "center",
      marginBottom: 8,
    },
    iconWrap: {
      width: 26,
      height: 26,
      borderRadius: 13,
      alignItems: "center",
      justifyContent: "center",
      marginRight: 8,
    },
    kind: {
      ...TypeScale.caption,
      fontWeight: "700",
      letterSpacing: 0,
      flexShrink: 1,
    },
    title: {
      ...TypeScale.body,
      color: chrome.text,
      fontWeight: "700",
    },
    detail: {
      ...TypeScale.compact,
      color: chrome.textMuted,
      marginTop: 2,
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
    time: {
      ...TypeScale.caption,
      color: chrome.textSubtle,
      marginLeft: "auto",
    },
  });
}
