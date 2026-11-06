import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { AnimatedChevron } from "../ui/AnimatedChevron";

interface InterfaceTimelineActivityExpandIconProps {
  expanded: boolean;
  chrome: TerminalThemeChrome;
}

/**
 * Native disclosure convention: a forward chevron while collapsed that turns
 * down once the details are open.
 */
export function InterfaceTimelineActivityExpandIcon({
  expanded,
  chrome,
}: InterfaceTimelineActivityExpandIconProps) {
  return (
    <AnimatedChevron
      expanded={expanded}
      size={13}
      color={expanded ? chrome.textMuted : chrome.textSubtle}
    />
  );
}
