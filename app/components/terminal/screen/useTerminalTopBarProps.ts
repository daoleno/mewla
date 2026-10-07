import { useMemo } from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../../constants/terminalThemes";
import type { AgentKind } from "../../../services/workerPresentation";
import type { TerminalFlavor } from "../../../services/terminalFlavor";
import type { StoredInterfaceRenderMode } from "../../../services/storage";
import type { WorkerStatus } from "../../../constants/tokens";
import type { TerminalTopBarProps } from "../TerminalTopBar";
import type { TerminalGitDiffSummary } from "../useTerminalGitDiff";
import type { useTerminalChromeLayout } from "./useTerminalChromeLayout";
import type { useTerminalNavigationActions } from "./useTerminalNavigationActions";

interface UseTerminalTopBarPropsInput {
  title: string;
  subtitle?: string;
  kind: AgentKind;
  terminalFlavor?: TerminalFlavor;
  terminalTheme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  chromeLayout: Pick<
    ReturnType<typeof useTerminalChromeLayout>,
    "menuAnchorRef" | "openMenu"
  >;
  navigationActions: Pick<
    ReturnType<typeof useTerminalNavigationActions>,
    "goBack"
  >;
  interfaceRenderMode: StoredInterfaceRenderMode;
  gitDiffDisabled: boolean;
  gitDiffSummary: TerminalGitDiffSummary | null;
  isStructuredChatWorker: boolean;
  delegated?: boolean;
  status?: WorkerStatus;
  onOpenSessionDetails(): void;
  openGitDiff(): void;
  onToggleInterfaceRenderMode(): void;
}

export function useTerminalTopBarProps({
  title,
  subtitle,
  kind,
  terminalFlavor,
  terminalTheme,
  chrome,
  chromeLayout,
  navigationActions,
  interfaceRenderMode,
  gitDiffDisabled,
  gitDiffSummary,
  isStructuredChatWorker,
  delegated,
  status,
  onOpenSessionDetails,
  openGitDiff,
  onToggleInterfaceRenderMode,
}: UseTerminalTopBarPropsInput): TerminalTopBarProps {
  return useMemo(
    () => ({
      title,
      subtitle,
      kind,
      terminalFlavor,
      backgroundColor: terminalTheme.background,
      chrome,
      menuAnchorRef: chromeLayout.menuAnchorRef,
      interfaceRenderMode,
      gitDiffDisabled,
      gitDiffPresentation: buildGitDiffPresentation({
        chrome,
        disabled: gitDiffDisabled,
        summary: gitDiffSummary,
        terminalTheme,
      }),
      isStructuredChatWorker,
      delegated,
      status,
      onBack: navigationActions.goBack,
      onOpenSessionDetails,
      onOpenGitDiff: openGitDiff,
      onOpenMenu: chromeLayout.openMenu,
      onToggleInterfaceRenderMode,
    }),
    [
      title,
      subtitle,
      kind,
      terminalFlavor,
      chrome,
      chromeLayout.menuAnchorRef,
      chromeLayout.openMenu,
      interfaceRenderMode,
      gitDiffDisabled,
      gitDiffSummary,
      isStructuredChatWorker,
      delegated,
      status,
      onOpenSessionDetails,
      openGitDiff,
      onToggleInterfaceRenderMode,
      navigationActions.goBack,
      terminalTheme.background,
      terminalTheme.green,
      terminalTheme.red,
      terminalTheme.yellow,
    ],
  );
}

function buildGitDiffPresentation({
  chrome,
  disabled,
  summary,
  terminalTheme,
}: {
  chrome: TerminalThemeChrome;
  disabled: boolean;
  summary: TerminalGitDiffSummary | null;
  terminalTheme: TerminalThemePalette;
}): TerminalTopBarProps["gitDiffPresentation"] {
  if (!summary) {
    return {
      accessibilityLabel: "Open Git diff",
      backgroundColor: "transparent",
      iconColor: disabled ? chrome.textSubtle : chrome.textMuted,
    };
  }

  // Uncommitted work is an agent's normal state: ink, not a warning.
  const toneColor =
    summary.tone === "dirty"
      ? chrome.text
      : summary.tone === "error"
        ? chrome.danger
        : chrome.textMuted;
  const statsLabel = summary.showStats
    ? `+${formatGitDelta(summary.additions)} -${formatGitDelta(summary.deletions)}`
    : "";

  return {
    accessibilityLabel: ["Open Git diff", summary.label, statsLabel]
      .filter(Boolean)
      .join(", "),
    additionsColor: terminalTheme.green,
    additionsText: summary.showStats
      ? `+${formatGitDelta(summary.additions)}`
      : undefined,
    backgroundColor: "transparent",
    deletionsColor: terminalTheme.red,
    deletionsText: summary.showStats
      ? `-${formatGitDelta(summary.deletions)}`
      : undefined,
    iconColor: disabled ? chrome.textSubtle : toneColor,
  };
}

function formatGitDelta(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "0";
  }
  if (value < 1000) {
    return String(value);
  }
  if (value < 10000) {
    return `${(value / 1000).toFixed(1).replace(/\.0$/, "")}k`;
  }
  return `${Math.round(value / 1000)}k`;
}
