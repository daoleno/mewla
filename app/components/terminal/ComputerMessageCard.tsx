import React, { useMemo, useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography, TypeScale } from "../../constants/tokens";
import { switchToComputer } from "../../services/computerSwitch";
import { useCurrentServer } from "../../store/currentServer";
import { chromeStatusInks } from "../brain/BrainWorkEventCard";
import { StatusMark } from "../ui/StatusMark";
import { workStatusInk, workStatusTextInk, type WorkStatus } from "../ui/workStatus";
import { InterfaceTimelineActivityExpandIcon } from "./InterfaceTimelineActivityExpandIcon";
import { useTimelineActivityExpansion } from "./InterfaceTimelineActivityExpansionState";
import { MessageBody } from "./InterfaceMessageBody";
import {
  serverForComputer,
  type ComputerMessageTimelineItem,
  type ComputerMessageTone,
} from "./computerMessageCardModel";

/**
 * A task handed to another of the user's computers, or its answer. Same slip
 * as Brain's other cards; "Open on <computer>" makes that computer current,
 * where its own Brain and Workers show what happened.
 */
export function ComputerMessageCard({
  item,
  chrome,
  theme,
}: {
  item: ComputerMessageTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const { servers, currentServerId, switchCurrentServer } = useCurrentServer();
  const [opening, setOpening] = useState(false);
  const target = serverForComputer(servers, item.daemonId, currentServerId);
  const expandable = item.body.length > item.preview.length || Boolean(item.userSaid);
  const { expanded, toggle } = useTimelineActivityExpansion(item.id, false);
  const status = TONE_STATUS[item.tone];
  const inks = chromeStatusInks(chrome);
  const time = item.timestamp ? formatChatBubbleTime(item.timestamp) : "";

  const open = async () => {
    if (!target || opening) return;
    setOpening(true);
    try {
      await switchToComputer(target.id, switchCurrentServer);
    } finally {
      setOpening(false);
    }
  };

  return (
    <Pressable
      accessibilityRole={expandable ? "button" : undefined}
      accessibilityLabel={[item.kindLabel, item.statusLabel, expanded ? item.body : item.preview].filter(Boolean).join(". ")}
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
        {item.statusLabel ? (
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
        ) : null}
      </View>

      {expanded ? (
        <View>
          {item.userSaid ? (
            <Text selectable style={styles.userSaid}>
              You said: {item.userSaid}
            </Text>
          ) : null}
          <MessageBody value={item.body} chrome={chrome} theme={theme} compact />
        </View>
      ) : (
        <Text numberOfLines={3} style={styles.preview}>
          {item.preview}
        </Text>
      )}

      {expandable || target ? (
        <View style={styles.footer}>
          {expandable ? (
            <>
              <InterfaceTimelineActivityExpandIcon expanded={expanded} chrome={chrome} />
              <Text style={styles.toggleLabel}>{expanded ? "Show less" : "Show all"}</Text>
            </>
          ) : null}
          <View style={styles.spacer} />
          {target ? (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={`Open on ${item.computer}`}
              disabled={opening}
              hitSlop={8}
              onPress={() => void open()}
            >
              <Text style={[styles.action, opening ? styles.actionBusy : null]}>Open on {item.computer}</Text>
            </Pressable>
          ) : null}
        </View>
      ) : null}
    </Pressable>
  );
}

// Delivered is the Ready check, not delivered the crossed box, and a
// message that may not have arrived the dashed ring; waiting has no mark.
const TONE_STATUS: Record<ComputerMessageTone, WorkStatus | null> = {
  accent: "ready",
  danger: "failed",
  attention: "blocked",
  neutral: null,
};

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
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
      fontFamily: Typography.uiFontMedium,
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
    userSaid: {
      ...TypeScale.compact,
      color: chrome.textMuted,
      marginBottom: 6,
    },
    preview: {
      ...TypeScale.compact,
      color: chrome.text,
      lineHeight: 20,
    },
    footer: {
      flexDirection: "row",
      alignItems: "center",
      gap: 5,
      marginTop: 10,
      minHeight: 18,
    },
    spacer: {
      flex: 1,
    },
    toggleLabel: {
      ...TypeScale.caption,
      color: chrome.textSubtle,
    },
    action: {
      ...TypeScale.caption,
      fontFamily: Typography.uiFontSemibold,
      color: chrome.accent,
    },
    actionBusy: {
      opacity: 0.5,
    },
  });
}
