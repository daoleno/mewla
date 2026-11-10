import React from "react";
import {
  StyleSheet,
  View,
  type LayoutChangeEvent,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { TerminalAccessoryDock } from "./TerminalAccessoryDock";
import { TerminalSurface, type TerminalSurfaceHandle } from "./TerminalSurface";
import { TerminalOutputStateCard } from "./TerminalOutputStateCard";
import { SessionFilePreviewSheet } from "./SessionFilePreviewSheet";

interface TerminalOutputPaneProps {
  sessionKey: string | null;
  serverId: string;
  workerId: string;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
  ctrlArmed: boolean;
  onCtrlArmedChange(next: boolean): void;
  canRenderTerminal: boolean;
  shouldMountTerminalSurface: boolean;
  terminalSurfaceActive: boolean;
  terminalStateFailed: boolean;
  terminalStateBusy: boolean;
  terminalStateTitle: string;
  terminalStateDetail: string;
  terminalStateHint: string;
  hasTerminalRoute: boolean;
  outputBottomInset: number;
  accessoryVisible: boolean;
  accessoryBottomOffset: number;
  serverUrl: string;
  daemonId: string;
  /** Which run of the Worker a tapped path belongs to. */
  workerProcessId?: number;
  workerStartedAt?: number;
  keyboardVisible: boolean;
  onRetryConnection(): void;
  onAccessoryLayout(event: LayoutChangeEvent): void;
}

function TerminalOutputPaneImpl({
  sessionKey,
  serverId,
  workerId,
  theme,
  chrome,
  terminalRef,
  ctrlArmed,
  onCtrlArmedChange,
  canRenderTerminal,
  shouldMountTerminalSurface,
  terminalSurfaceActive,
  terminalStateFailed,
  terminalStateBusy,
  terminalStateTitle,
  terminalStateDetail,
  terminalStateHint,
  hasTerminalRoute,
  outputBottomInset,
  accessoryVisible,
  accessoryBottomOffset,
  serverUrl,
  daemonId,
  workerProcessId,
  workerStartedAt,
  keyboardVisible,
  onRetryConnection,
  onAccessoryLayout,
}: TerminalOutputPaneProps) {
  const [filePreviewReference, setFilePreviewReference] = React.useState<string | null>(null);
  const openPath = React.useCallback((path: string) => {
    if (path && path.length <= 4096 && !path.includes("\u0000")) setFilePreviewReference(path);
  }, []);
  const closeFilePreview = React.useCallback(() => setFilePreviewReference(null), []);
  const hasTerminalTarget = Boolean(sessionKey && serverId && workerId);
  const shouldAutoResumeTerminal =
    terminalSurfaceActive && canRenderTerminal && hasTerminalTarget;

  React.useEffect(() => {
    if (!shouldMountTerminalSurface || !hasTerminalTarget) {
      return;
    }

    if (!terminalSurfaceActive) {
      terminalRef.current?.blur();
      return;
    }

    const frame = requestAnimationFrame(() => {
      // resumeInput wakes the WebView compositor then restores live input.
      terminalRef.current?.resumeInput();
    });
    return () => {
      cancelAnimationFrame(frame);
    };
  }, [
    workerId,
    hasTerminalTarget,
    serverId,
    sessionKey,
    shouldAutoResumeTerminal,
    shouldMountTerminalSurface,
    terminalRef,
    terminalSurfaceActive,
  ]);

  return (
    <>
      <View
        collapsable={false}
        pointerEvents={terminalSurfaceActive ? "auto" : "none"}
        style={[
          styles.output,
          { backgroundColor: theme.background },
          outputBottomInset > 0 ? { paddingBottom: outputBottomInset } : null,
        ]}
      >
        {shouldMountTerminalSurface && sessionKey && serverId && workerId ? (
          <TerminalSurface
            key={sessionKey}
            ref={terminalRef}
            serverId={serverId}
            targetId={workerId}
            theme={theme}
            ctrlArmed={ctrlArmed}
            onCtrlArmedChange={onCtrlArmedChange}
            onOpenPath={workerProcessId && workerStartedAt ? openPath : undefined}
          />
        ) : null}
        {canRenderTerminal ? null : (
          <TerminalOutputStateCard
            failed={terminalStateFailed}
            busy={terminalStateBusy}
            title={terminalStateTitle}
            detail={terminalStateDetail}
            hint={terminalStateHint}
            showRetry={hasTerminalRoute}
            onRetry={onRetryConnection}
          />
        )}
      </View>

      {accessoryVisible ? (
        <TerminalAccessoryDock
          terminalRef={terminalRef}
          uploadOwnerKey={sessionKey}
          serverId={serverId}
          serverUrl={serverUrl}
          daemonId={daemonId}
          theme={theme}
          keyboardVisible={keyboardVisible}
          ctrlArmed={ctrlArmed}
          bottomOffset={accessoryBottomOffset}
          onCtrlArmedChange={onCtrlArmedChange}
          onLayout={onAccessoryLayout}
        />
      ) : null}

      {workerProcessId && workerStartedAt ? (
        <SessionFilePreviewSheet
          reference={filePreviewReference}
          serverId={serverId}
          serverUrl={serverUrl}
          daemonId={daemonId}
          workerId={workerId}
          processId={workerProcessId}
          startedAt={workerStartedAt}
          chrome={chrome}
          theme={theme}
          onClose={closeFilePreview}
        />
      ) : null}
    </>
  );
}

export const TerminalOutputPane = React.memo(TerminalOutputPaneImpl);

const styles = StyleSheet.create({
  output: {
    flex: 1,
    minHeight: 0,
    paddingTop: 4,
  },
});