import { useCallback } from "react";
import { confirmDestructive } from "../../ui/confirmDestructive";
import { useRouter } from "expo-router";
import type { Worker, ConnectionState } from "../../../store/workers";
import { dismissTerminalToSessions } from "../../../services/terminalExitNavigation";
import { useScreenBack } from "../../navigation/useScreenBack";
import { wsClient } from "../../../services/websocket";

import { useToast } from "../../ui/Toast";
interface UseTerminalNavigationActionsInput {
  sessionKey: string | null;
  serverId: string;
  workerId: string;
  connectionState: ConnectionState;
  displayName: string;
  workerServerName?: string;
  closeMenu(): void;
}

export function useTerminalNavigationActions({
  sessionKey,
  serverId,
  workerId,
  connectionState,
  displayName,
  workerServerName,
  closeMenu,
}: UseTerminalNavigationActionsInput) {
  const toast = useToast();
  const router = useRouter();
  const screenBack = useScreenBack({ parent: "/list" });

  const goBack = useCallback(() => {
    closeMenu();
    screenBack();
  }, [closeMenu, screenBack]);

  const performTerminateWorker = useCallback(async () => {
    if (!sessionKey || !serverId || !workerId) return;

    wsClient.killWorker(serverId, workerId);
    dismissTerminalToSessions(router);
  }, [workerId, router, serverId, sessionKey]);

  const handleTerminateWorker = useCallback(() => {
    if (!sessionKey || !serverId || !workerId) return;

    closeMenu();

    if (connectionState !== "connected") {
      toast.show({ title: "Daemon unavailable", detail: "Reconnect to that daemon before terminating the agent.", tone: "error" });
      return;
    }

    confirmDestructive({
      title: "Terminate session?",
      message:
        "This will terminate " +
        (displayName || workerId) +
        " on " +
        (workerServerName || serverId) +
        ".",
      confirmLabel: "Terminate",
      onConfirm: performTerminateWorker,
    });
  }, [
    workerId,
    workerServerName,
    closeMenu,
    connectionState,
    displayName,
    performTerminateWorker,
    serverId,
    sessionKey,
  ]);

  return {
    goBack,
    handleTerminateWorker,
  };
}
