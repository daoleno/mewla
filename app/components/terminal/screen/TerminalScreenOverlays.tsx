import React from "react";
import { StyleSheet } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../../constants/terminalThemes";
import type {
  ProviderError,
  ThreadRuntimeChoice,
} from "../../../services/providers";
import type { ProviderPickerModelRow } from "../../../services/providers/sessionModelHelpers";
import { InlineNotice } from "../../ui/InlineNotice";
import { SessionModelSheet } from "../../providers/SessionModelSheet";
import { GitDiffSheet } from "../GitDiffSheet";
import { NewTerminalSheet } from "../NewTerminalSheet";
import { TerminalActionPopover } from "../TerminalActionPopover";
import { TerminalRenameModal } from "../TerminalRenameModal";

type GitDiffSheetProps = React.ComponentProps<typeof GitDiffSheet>;
type NewTerminalSubmitInput = Parameters<
  React.ComponentProps<typeof NewTerminalSheet>["onSubmit"]
>[0];

export interface TerminalScreenOverlaysProps {
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
  newTerminalDisabled: boolean;
  showLinkedWork: boolean;
  showToggleRenderMode?: boolean;
  toggleRenderModeLabel?: string;
  newTerminalVisible: boolean;
  newTerminalInitialCwd: string;
  selectedServerId: string;
  gitDiffSheetProps: Omit<GitDiffSheetProps, "theme">;
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
  onNewTerminal(): void;
  onCloseMenu(): void;
  onRename(): void;
  onOpenLinkedWork(): void;
  onToggleRenderMode?(): void;
  onTerminate(): void;
  onCloseNewTerminal(): void;
  onSubmitNewTerminal(input: NewTerminalSubmitInput): void;
  onRenameDraftChange(value: string): void;
  onCloseRename(): void;
  onSaveRename(): void;
}

export function TerminalScreenOverlays({
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
  newTerminalDisabled,
  showLinkedWork,
  showToggleRenderMode = false,
  toggleRenderModeLabel,
  newTerminalVisible,
  newTerminalInitialCwd,
  selectedServerId,
  gitDiffSheetProps,
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
  onNewTerminal,
  onCloseMenu,
  onRename,
  onOpenLinkedWork,
  onToggleRenderMode,
  onTerminate,
  onCloseNewTerminal,
  onSubmitNewTerminal,
  onRenameDraftChange,
  onCloseRename,
  onSaveRename,
}: TerminalScreenOverlaysProps) {
  return (
    <>
      {createDurabilityWarning ? (
        <InlineNotice
          tone="warning"
          title="Not confirmed durable"
          detail={createDurabilityWarning}
          detailNumberOfLines={0}
          style={styles.durabilityBanner}
          action={
            onDismissCreateDurabilityWarning
              ? { label: "Dismiss", accessibilityLabel: "Dismiss durability warning", onPress: onDismissCreateDurabilityWarning }
              : undefined
          }
        />
      ) : null}

      <SessionModelSheet
        visible={routeSheetVisible}
        loading={routeSheetLoading}
        activating={routeSheetActivating}
        error={routeSheetError}
        rows={routeSheetRows}
        onClose={onCloseRouteSheet}
        onRetry={onRetryRouteSheet}
        onActivate={onActivateSessionModel}
      />

      <TerminalActionPopover
        visible={menuVisible}
        left={menuPosition.left}
        top={menuPosition.top}
        title={menuTitle}
        creatingSession={creatingSession}
        newTerminalLabel={creatingSession ? "Starting Terminal…" : "New Terminal"}
        newTerminalDisabled={newTerminalDisabled}
        showLinkedWork={showLinkedWork}
        showToggleRenderMode={showToggleRenderMode}
        toggleRenderModeLabel={toggleRenderModeLabel}
        chrome={chrome}
        theme={theme}
        onClose={onCloseMenu}
        onNewTerminal={onNewTerminal}
        onRename={onRename}
        onOpenLinkedWork={onOpenLinkedWork}
        onOpenModel={onOpenModel}
        onOpenDSHWeb={onOpenDSHWeb}
        onToggleRenderMode={onToggleRenderMode}
        onTerminate={onTerminate}
      />

      <NewTerminalSheet
        key={selectedServerId ?? "no-server"}
        visible={newTerminalVisible}
        title="New session"
        initialCwd={newTerminalInitialCwd}
        serverId={selectedServerId}
        submitting={creatingSession}
        onClose={onCloseNewTerminal}
        onSubmit={onSubmitNewTerminal}
      />

      <GitDiffSheet
        theme={theme}
        {...gitDiffSheetProps}
      />

      <TerminalRenameModal
        visible={renameVisible}
        draft={renameDraft}
        placeholder={renamePlaceholder}
        onDraftChange={onRenameDraftChange}
        onClose={onCloseRename}
        onSave={onSaveRename}
      />
    </>
  );
}

const styles = StyleSheet.create({
  durabilityBanner: { marginHorizontal: 12, marginTop: 8, marginBottom: 4 },
});
