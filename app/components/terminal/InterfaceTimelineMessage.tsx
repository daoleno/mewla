import React from "react";
import { StyleSheet, Text, View } from "react-native";

import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  ContinuousCorners,
  TypeScale,
  useAppTheme,
} from "../../constants/tokens";
import { mixHex } from "../../theme/colorUtils";
import type { MessagePresentation } from "./InterfaceTimelineGrouping";
import { MessageBubbleFooter } from "./MessageBubbleFooter";
import {
  chatgptUserBubbleRadii,
  messageRowSpacing,
  userBubbleRadii,
} from "./messageBubbleShape";

import { MessageBody } from "./InterfaceMessageBody";
import { InterfaceTimelineAttachmentPreviewList } from "./InterfaceTimelineAttachmentPreviewList";
import { PendingSendStatusMark } from "./PendingSendStatusMark";
import {
  PENDING_SEND_STATUS_MARK_SIZE,
  PENDING_SEND_STATUS_OUTSIDE_RIGHT,
} from "./pendingSendStatusGeometry";
import { showsPendingSendStatusMark } from "./pendingUserMessageLifecycle";

export type DisplayAttachment = {
  name: string;
  path: string;
  localUri?: string;
  mimeType?: string;
};

export interface MessageTimelineItem {
  type: "message";
  id: string;
  role: "user" | "assistant";
  timestamp?: string;
  body: string;
  attachments: DisplayAttachment[];
  pending?: boolean;
  pendingLifecycle?: "pending" | "failed";
  pendingLifecycleLabel?: string;
  pendingFailureMessage?: string;
  onRetryPending?: () => void;
  streaming?: boolean;

  /** Process-local presentation alias; provider id/body remain canonical. */
  turnFocusAnchorId?: string;
}

const DEFAULT_PRESENTATION: MessagePresentation = {
  showAvatar: false,
  groupPosition: "single",
  compactTop: false,
  compactBottom: false,
};

export function UserMessage({
  item,
  presentation = DEFAULT_PRESENTATION,
  chrome,
  theme,
}: {
  item: MessageTimelineItem & { role: "user" };
  presentation?: MessagePresentation;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const { theme: appTheme } = useAppTheme();
  const chatLayout = appTheme.chat.layout;
  const isChatGpt = chatLayout === "chatgpt";

  const hasBody = item.body.trim().length > 0;
  const sentBubbleColor = appTheme.chat.sentBubble;
  // Code, tables and attachments inside the bubble sit on bubble-derived
  // wells: the light theme's sent bubble is ink, so canvas fills would glare.
  const sentChrome = {
    ...chrome,
    text: appTheme.chat.sentText,
    textMuted: appTheme.chat.sentTimestamp,
    textSubtle: appTheme.chat.sentTimestamp,
    link: appTheme.chat.sentText,
    surface: mixHex(sentBubbleColor, appTheme.chat.sentText, 0.08),
    surfaceMuted: mixHex(sentBubbleColor, appTheme.chat.sentText, 0.12),
    border: mixHex(sentBubbleColor, appTheme.chat.sentText, 0.2),
  };
  const spacing = messageRowSpacing(
    presentation.compactTop,
    presentation.compactBottom,
    chatLayout,
    "user",
  );
  const bubbleRadii = isChatGpt
    ? chatgptUserBubbleRadii()
    : userBubbleRadii(presentation.groupPosition);
  const showPendingSendMark = showsPendingSendStatusMark({
    pending: item.pending,
    lifecycle: item.pendingLifecycle,
  });
  const showFooter =
    item.pendingLifecycle === "failed" ||
    Boolean(item.pendingLifecycleLabel) ||
    Boolean(item.onRetryPending) ||
    appTheme.chat.showTimestamps;

  return (
    <View style={[styles.userRow, spacing]}>
      <View
        // Busy only — never a status-only accessibilityLabel that replaces body.
        accessibilityState={item.pending ? { busy: true } : undefined}
        style={[
          isChatGpt ? styles.userBubbleChatGpt : styles.userBubble,
          bubbleRadii,
          { backgroundColor: sentBubbleColor },
        ]}
      >
        {hasBody ? (
          <MessageBody value={item.body} chrome={sentChrome} theme={theme} />
        ) : null}
        {item.attachments.length > 0 ? (
          <InterfaceTimelineAttachmentPreviewList
            attachments={item.attachments}
            chrome={sentChrome}
            compact={hasBody}
          />
        ) : null}
        {showFooter ? (
          <MessageBubbleFooter
            timestamp={item.timestamp}
            tone="sent"
            lifecycleLabel={item.pendingLifecycleLabel}
            failureMessage={item.pendingFailureMessage}
            failureColor={chrome.danger}
            onRetry={item.onRetryPending}
          />
        ) : null}
        {showPendingSendMark ? (
          <View style={styles.pendingSendMark} pointerEvents="none">
            <PendingSendStatusMark color={appTheme.chat.outboundSentClock} />
          </View>
        ) : null}
      </View>
    </View>
  );
}

export function AssistantMessage({
  item,
  presentation = DEFAULT_PRESENTATION,
  chrome,
  theme,
  senderLabel,
}: {
  item: MessageTimelineItem & { role: "assistant" };
  presentation?: MessagePresentation;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  senderLabel?: string;
}) {
  const { theme: appTheme } = useAppTheme();
  const chatLayout = appTheme.chat.layout;
  const spacing = messageRowSpacing(
    presentation.compactTop,
    presentation.compactBottom,
    chatLayout,
    "assistant",
  );
  const assistantChrome = {
    ...chrome,
    text: appTheme.chat.receivedText,
    link: appTheme.chat.link,
  };
  const showSender =
    senderLabel &&
    presentation.groupPosition !== "middle" &&
    presentation.groupPosition !== "last";

  return (
    <View style={[styles.assistantRow, spacing]}>
      {showSender ? (
        <Text style={[styles.assistantSender, { color: chrome.accent }]}>
          {senderLabel}
        </Text>
      ) : null}
      <View style={styles.assistantContent}>
        <MessageBody
          value={item.body}
          chrome={assistantChrome}
          theme={theme}
          streaming={item.streaming}
        />
        {appTheme.chat.showTimestamps ? (
          <MessageBubbleFooter timestamp={item.timestamp} tone="received" />
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  userRow: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
    flexDirection: "row",
    justifyContent: "flex-end",
    overflow: "visible",
  },
  userBubble: {
    position: "relative",
    maxWidth: "86%",
    overflow: "visible",
    paddingHorizontal: 14,
    paddingTop: 9,
    paddingBottom: 9,
    ...ContinuousCorners,
  },
  userBubbleChatGpt: {
    position: "relative",
    maxWidth: "88%",
    overflow: "visible",
    paddingHorizontal: 14,
    paddingVertical: 10,
    ...ContinuousCorners,
  },
  pendingSendMark: {
    position: "absolute",
    right: PENDING_SEND_STATUS_OUTSIDE_RIGHT,
    bottom: 0,
    width: PENDING_SEND_STATUS_MARK_SIZE,
    height: PENDING_SEND_STATUS_MARK_SIZE,
    alignItems: "center",
    justifyContent: "center",
  },
  assistantRow: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
  },
  assistantSender: {
    ...TypeScale.micro,
    marginBottom: 4,
  },
  assistantContent: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
  },

});
