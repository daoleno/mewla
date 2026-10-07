import { useCallback, useMemo } from "react";
import type {
  ProviderError,
  ThreadRuntimeChoice,
} from "../../../services/providers";
import type { ProviderPickerModelRow } from "../../../services/providers/sessionModelHelpers";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../../constants/terminalThemes";
import type { useTerminalGitDiff } from "../useTerminalGitDiff";
import type { TerminalScreenOverlaysProps } from "./TerminalScreenOverlays";
import type { useTerminalSessionActions } from "./useTerminalSessionActions";
import type { useTerminalNavigationActions } from "./useTerminalNavigationActions";

interface UseTerminalScreenOverlayPropsInput {
  menuTitle: string;
  routeSheetVisible: boolean;
  routeSheetLoading: boolean;
  routeSheetActivating: boolean;
  routeSheetError?: ProviderError | string | null;
  routeSheetRows: ProviderPickerModelRow[];
  createDurabilityWarning?: string | null;
  onDismissCreateDurabilityWarning?(): void;
  creatingSession: boolean;
  menuVisible: boolean;
  menuPosition: { left: number; top: number };
  connectionConnected: boolean;
  gitDiff: ReturnType<typeof useTerminalGitDiff>;
  hasLinkedWork: boolean;
  showToggleRenderMode?: boolean;
  toggleRenderModeLabel?: string;
  newTerminalVisible: boolean;
  workerCwd?: string;
  serverId: string;
  renameVisible: boolean;
  renameDraft: string;
  renamePlaceholder: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onCloseRouteSheet(): void;
  onRetryRouteSheet(): void;
  onActivateSessionModel(choice: ThreadRuntimeChoice): void;
  onOpenModel?(): void;
  onOpenDSHWeb?(): void;
  setNewTerminalVisible(value: boolean): void;
  setRenameVisible(value: boolean): void;
  setRenameDraft(value: string): void;
  navigationActions: ReturnType<typeof useTerminalNavigationActions>;
  sessionActions: ReturnType<typeof useTerminalSessionActions>;
  closeMenu(): void;
  openNewTerminal(): void;
  openRenameModal(): void;
  onToggleRenderMode?(): void;
}

export function useTerminalScreenOverlayProps({
  menuTitle,
  routeSheetVisible,
  routeSheetLoading,
  routeSheetActivating,
  routeSheetError,
  routeSheetRows,
  createDurabilityWarning,
  onDismissCreateDurabilityWarning,
  creatingSession,
  menuVisible,
  menuPosition,
  connectionConnected,
  gitDiff,
  hasLinkedWork,
  showToggleRenderMode = false,
  toggleRenderModeLabel,
  newTerminalVisible,
  workerCwd,
  serverId,
  renameVisible,
  renameDraft,
  renamePlaceholder,
  chrome,
  theme,
  onCloseRouteSheet,
  onRetryRouteSheet,
  onActivateSessionModel,
  onOpenModel,
  onOpenDSHWeb,
  setNewTerminalVisible,
  setRenameVisible,
  setRenameDraft,
  navigationActions,
  sessionActions,
  closeMenu,
  openNewTerminal,
  openRenameModal,
  onToggleRenderMode,
}: UseTerminalScreenOverlayPropsInput): TerminalScreenOverlaysProps {
  const handleCloseNewTerminal = useCallback(() => {
    setNewTerminalVisible(false);
  }, [setNewTerminalVisible]);

  const handleSubmitNewTerminal = useCallback(
    (input: Parameters<TerminalScreenOverlaysProps["onSubmitNewTerminal"]>[0]) => {
      void sessionActions.createTerminal({
        cwd: input.cwd,
        command: input.command,
        name: input.name,
      });
    },
    [sessionActions.createTerminal],
  );

  const handleCloseRename = useCallback(() => {
    setRenameVisible(false);
  }, [setRenameVisible]);

  return useMemo(
    () => ({
      menuTitle,
      routeSheetVisible,
      routeSheetLoading,
      routeSheetActivating,
      routeSheetError,
      routeSheetRows,
      createDurabilityWarning,
      onDismissCreateDurabilityWarning,
      creatingSession,
      menuVisible,
      menuPosition,
      newTerminalDisabled: !connectionConnected,
      showLinkedWork: hasLinkedWork,
      showToggleRenderMode,
      toggleRenderModeLabel,
      newTerminalVisible,
      newTerminalInitialCwd: workerCwd || "",
      selectedServerId: serverId,
      gitDiffSheetProps: gitDiff.sheetProps,
      renameVisible,
      renameDraft,
      renamePlaceholder,
      chrome,
      theme,
      onCloseRouteSheet,
      onRetryRouteSheet,
      onActivateSessionModel,
      onOpenModel,
      onOpenDSHWeb,
      onNewTerminal: openNewTerminal,
      onCloseMenu: closeMenu,
      onRename: openRenameModal,
      onOpenLinkedWork: sessionActions.openLinkedWork,
      onToggleRenderMode,
      onTerminate: navigationActions.handleTerminateWorker,
      onCloseNewTerminal: handleCloseNewTerminal,
      onSubmitNewTerminal: handleSubmitNewTerminal,
      onRenameDraftChange: setRenameDraft,
      onCloseRename: handleCloseRename,
      onSaveRename: sessionActions.handleSaveRename,
    }),
    [
      workerCwd,
      chrome,
      menuTitle,
      closeMenu,
      connectionConnected,
      creatingSession,
      createDurabilityWarning,
      onDismissCreateDurabilityWarning,
      gitDiff.sheetProps,
      handleCloseNewTerminal,
      handleCloseRename,
      handleSubmitNewTerminal,
      hasLinkedWork,
      menuPosition,
      menuVisible,
      navigationActions.handleTerminateWorker,
      newTerminalVisible,
      onActivateSessionModel,
      onCloseRouteSheet,
      onOpenModel,
      onOpenDSHWeb,
      onRetryRouteSheet,
      onToggleRenderMode,
      openNewTerminal,
      openRenameModal,
      renameDraft,
      renamePlaceholder,
      renameVisible,
      routeSheetActivating,
      routeSheetError,
      routeSheetRows,
      routeSheetLoading,
      routeSheetVisible,
      serverId,
      sessionActions.handleSaveRename,
      sessionActions.openLinkedWork,
      setRenameDraft,
      showToggleRenderMode,
      theme,
      toggleRenderModeLabel,
    ],
  );
}
