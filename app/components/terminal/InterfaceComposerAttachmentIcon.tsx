import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
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
        looksLikeImagePath(fileName)
          ? "image"
          : "document-text"
      }
      size={size}
      color={color ?? chrome.textMuted}
    />
  );
}

function looksLikeImagePath(value: string) {
  return /\.(png|jpe?g|gif|webp|bmp)$/i.test(value.trim());
}
