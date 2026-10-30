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
import { describeGitDiffTopBarButton } from "../gitDiffPresentation";
import type { TerminalGitDiffSummary } from "../useTerminalGitDiff";
import type { useTerminalChromeLayout } from "./useTerminalChromeLayout";
import type { useTerminalNavigationActions } from "./useTerminalNavigationActions";

interface UseTerminalTopBarPropsInput {
  title: string;
  subtitle?: string;
  calendarOrigin?: TerminalTopBarProps["calendarOrigin"];
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
  openGitDiff(): void;
  onToggleInterfaceRenderMode(): void;
}

export function useTerminalTopBarProps({
  title,
  subtitle,
  calendarOrigin,
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
  openGitDiff,
  onToggleInterfaceRenderMode,
}: UseTerminalTopBarPropsInput): TerminalTopBarProps {
  return useMemo(
    () => ({
      title,
      subtitle,
      calendarOrigin,
      kind,
      terminalFlavor,
      backgroundColor: terminalTheme.background,
      chrome,
      menuAnchorRef: chromeLayout.menuAnchorRef,
      interfaceRenderMode,
      gitDiffDisabled,
      gitDiffPresentation: describeGitDiffTopBarButton({
        chrome,
        disabled: gitDiffDisabled,
        summary: gitDiffSummary,
        terminalTheme,
      }),
      isStructuredChatWorker,
      delegated,
      status,
      onBack: navigationActions.goBack,
      onOpenGitDiff: openGitDiff,
      onOpenMenu: chromeLayout.openMenu,
      onToggleInterfaceRenderMode,
    }),
    [
      title,
      subtitle,
      calendarOrigin,
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
