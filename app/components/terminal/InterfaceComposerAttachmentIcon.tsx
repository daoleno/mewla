import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { isImageAttachment } from "../../services/imageSource";
import { Icon } from "../icons/Icon";

interface InterfaceComposerAttachmentIconProps {
  fileName: string;
  chrome: TerminalThemeChrome;
  color?: string;
  size?: number;
}

export function InterfaceComposerAttachmentIcon({
  fileName,
  chrome,
  color,
  size = 17,
}: InterfaceComposerAttachmentIconProps) {
  return (
    <Icon
      name={
        isImageAttachment({ name: fileName })
          ? "image"
          : "document-text"
      }
      size={size}
      color={color ?? chrome.textMuted}
    />
  );
}
