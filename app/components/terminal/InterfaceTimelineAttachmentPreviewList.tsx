import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { DisplayAttachment } from "./InterfaceTimelineMessage";

import { AppImage } from "./AppImage";
import { imageReference, isImageAttachment, type ImageSource } from "../../services/imageSource";
import { Icon } from "../icons/Icon";
import { pathBasename } from "../../services/pathDisplay";

function attachmentSource(attachment: DisplayAttachment): ImageSource {
  return attachment.localUri ? { kind: "phone", uri: attachment.localUri, name: attachment.name, mimeType: attachment.mimeType } : imageReference(attachment.path, attachment.name, attachment.mimeType);
}

interface InterfaceTimelineAttachmentPreviewListProps {
  attachments: DisplayAttachment[];
  chrome: TerminalThemeChrome;
  compact?: boolean;
}

export function InterfaceTimelineAttachmentPreviewList({
  attachments,
  chrome,
  compact,
}: InterfaceTimelineAttachmentPreviewListProps) {
  const gallery = attachments.filter(isImageAttachment).map(attachmentSource);
  return (
    <View
      style={[styles.attachments, compact ? styles.attachmentsCompact : null]}
    >
      {attachments.map((attachment) => (
        <InterfaceTimelineAttachmentPreviewPill
          key={`${attachment.name}:${attachment.path}:${attachment.localUri ?? ""}`}
          attachment={attachment}
          chrome={chrome}
          gallery={gallery}
        />
      ))}
    </View>
  );
}

function InterfaceTimelineAttachmentPreviewPill({
  attachment,
  chrome,
  gallery,
}: {
  attachment: DisplayAttachment;
  gallery: ImageSource[];
  chrome: TerminalThemeChrome;
}) {
  if (isImageAttachment(attachment)) {
    return <AppImage source={attachmentSource(attachment)} gallery={gallery} chrome={chrome} />;
  }

  return (
    <View
      style={[
        styles.attachmentPill,
        {
          borderColor: chrome.border,
          backgroundColor: chrome.surfaceMuted,
        },
      ]}
    >
      <Icon
        name="attach"
        size={13}
        color={chrome.textSubtle}
      />
      <Text
        style={[styles.attachmentPillText, { color: chrome.textMuted }]}
        numberOfLines={1}
      >
        {attachment.name || pathBasename(attachment.path)}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  attachments: {
    gap: 7,
    flexDirection: "row",
    flexWrap: "wrap",
  },
  attachmentsCompact: {
    marginTop: 8,
  },
  attachmentPill: {
    alignSelf: "flex-start",
    maxWidth: "100%",
    minHeight: 30,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 9,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  thumbPill: {
    width: 84,
    height: 84,
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  thumb: {
    width: "100%",
    height: "100%",
  },
  attachmentPillText: {
    flexShrink: 1,
    fontSize: 11.5,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
});
