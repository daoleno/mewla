import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Icon } from "../icons/Icon";

interface InterfaceTimelineActivityExpandIconProps {
  expanded: boolean;
  chrome: TerminalThemeChrome;
}

/**
 * Native disclosure convention: a forward chevron while collapsed, a down
 * chevron once the details are open.
 */
export function InterfaceTimelineActivityExpandIcon({
  expanded,
  chrome,
}: InterfaceTimelineActivityExpandIconProps) {
  return (
    <Icon
      name={expanded ? "chevron-down" : "chevron-right"}
      size={13}
      color={expanded ? chrome.textMuted : chrome.textSubtle}
    />
  );
}
