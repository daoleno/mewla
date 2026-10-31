import React, { useMemo } from "react";
import type { ViewInstance } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { AgentKind } from "../../services/workerPresentation";
import type { TerminalFlavor } from "../../services/terminalFlavor";
import type { StoredInterfaceRenderMode } from "../../services/storage";
import type { WorkerStatus } from "../../constants/tokens";
import { TelegramChatHeader } from "./TelegramChatHeader";
import { useDesktopWeb } from "../navigation/useDesktopWeb";

export interface TerminalTopBarGitDiffPresentation {
  accessibilityLabel: string;
  backgroundColor: string;
  iconColor: string;
  additionsText?: string;
  additionsColor?: string;
  deletionsText?: string;
  deletionsColor?: string;
  /** A dot on the icon for changes with no countable lines. */
  markColor?: string;
}

export interface TerminalTopBarProps {
  title: string;
  subtitle?: string;
  /** Calendar-born work: "运营：挖稿 · Oct 27" under the title, tapping opens it. */
  calendarOrigin?: { label: string; title: string; date: string; open(): void } | null;
  kind: AgentKind;
  terminalFlavor?: TerminalFlavor;
  backgroundColor: string;
  chrome: TerminalThemeChrome;
  menuAnchorRef: React.RefObject<ViewInstance | null>;
  interfaceRenderMode: StoredInterfaceRenderMode;
  gitDiffDisabled: boolean;
  gitDiffPresentation: TerminalTopBarGitDiffPresentation;
  isStructuredChatWorker: boolean;
  delegated?: boolean;
  status?: WorkerStatus;
  onBack(): void;
  onOpenGitDiff(): void;
  onOpenMenu(): void;
  onToggleInterfaceRenderMode(): void;
}

export function TerminalTopBar({
  title,
  subtitle,
  calendarOrigin,
  kind,
  terminalFlavor,
  chrome,
  menuAnchorRef,
  gitDiffDisabled,
  gitDiffPresentation,
  delegated,
  status,
  onBack,
  onOpenGitDiff,
  onOpenMenu,
}: TerminalTopBarProps) {
  // Desktop web: the page header's flat bar, not floating phone capsules.
  const desktopWeb = useDesktopWeb();
  const rightActions = useMemo(
    () => [
      {
        key: "git-diff",
        icon: "git-branch" as const,
        accessibilityLabel: gitDiffPresentation.accessibilityLabel,
        disabled: gitDiffDisabled,
        iconColor: gitDiffPresentation.iconColor,
        markColor: gitDiffPresentation.markColor,
        stats: [
          { text: gitDiffPresentation.additionsText, color: gitDiffPresentation.additionsColor },
          { text: gitDiffPresentation.deletionsText, color: gitDiffPresentation.deletionsColor },
        ].filter(
          (stat): stat is { text: string; color: string | undefined } => Boolean(stat.text),
        ),
        onPress: onOpenGitDiff,
      },
      {
        key: "menu",
        icon: "more-vertical" as const,
        accessibilityLabel: "Session actions",
        onPress: onOpenMenu,
      },
    ],
    [
      gitDiffDisabled,
      gitDiffPresentation.accessibilityLabel,
      gitDiffPresentation.iconColor,
      gitDiffPresentation.markColor,
      gitDiffPresentation.additionsText,
      gitDiffPresentation.additionsColor,
      gitDiffPresentation.deletionsText,
      gitDiffPresentation.deletionsColor,
      onOpenGitDiff,
      onOpenMenu,
    ],
  );

  return (
    <TelegramChatHeader
      chrome={chrome}
      title={title}
      subtitle={
        calendarOrigin
          ? // The title already names the schedule when the Session took it.
            calendarOrigin.title === title
            ? calendarOrigin.date || calendarOrigin.label
            : calendarOrigin.label
          : subtitle ?? (delegated ? "Delegated" : undefined)
      }
      subtitleIcon={calendarOrigin ? "calendar" : undefined}
      onPressTitle={calendarOrigin?.open}
      titleActionLabel={calendarOrigin ? "Open in Calendar" : undefined}
      agentKind={kind}
      terminalFlavor={terminalFlavor}
      avatarLabel={title}
      avatarSeed={title}
      status={status}
      onBack={onBack}
      rightActions={rightActions}
      menuAnchorRef={menuAnchorRef}
      flat={desktopWeb}
    />
  );
}
