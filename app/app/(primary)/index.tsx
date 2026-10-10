import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ScrollView, StyleSheet, View, useWindowDimensions } from "react-native";
import {
  useIsFocused,
  useLocalSearchParams,
  useRouter,
} from "expo-router";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import {
  BrainExecutorSheet,
} from "../../components/brain/BrainExecutorSheet";
import { BrainExecutorIcon } from "../../components/brain/BrainExecutorIcon";
import { BrainExecutorMentionPicker } from "../../components/brain/BrainExecutorMentionPicker";
import { BrainWorkspaceViewer } from "../../components/brain/BrainWorkspaceViewer";
import { BrainWorkEventDetailSheet } from "../../components/brain/BrainWorkEventDetailSheet";
import { SessionModelSheet } from "../../components/providers/SessionModelSheet";
import { useSessionProviderSheet } from "../../components/terminal/screen/useSessionProviderSheet";
import {
  brainProviderLabel,
  switchExecutorAccessibilityLabel,
} from "../../components/brain/brainPresentation";
import { usePrimaryPageAction } from "../../components/navigation/PrimaryPageAction";
import {
  PRIMARY_SIDEBAR_BREAKPOINT,
  resolvePrimaryAppBarGeometry,
} from "../../components/navigation/PrimaryDrawerShell";
import {
  BrainWorkColumn,
  BrainWorkDetailSheet,
  BrainWorkSheet,
  type BrainWorkReveal,
} from "../../components/brain/BrainWorkPanel";
import {
  BRAIN_WORK_GROUP_ORDER,
  brainWorkSlipStale,
  brainWorkSurface,
  workerWho,
  type BrainWorkSlip,
} from "../../components/brain/brainWorkSurface";
import { ActionMenu, EmptyState, InlineNotice } from "../../components/ui";
import { setServerAutoConnect } from "../../services/storage";
import { ChatCanvas } from "../../components/terminal/ChatCanvas";
import { CHAT_CHROME_HORIZONTAL_INSET } from "../../components/terminal/chatChromeMetrics";
import { InterfaceChatSurface } from "../../components/terminal/InterfaceChatSurface";
import { buildChatChrome } from "../../theme";
import { useAppTheme } from "../../constants/tokens";
import { wsClient } from "../../services/websocket";
import { terminalRouteParams } from "../../services/workerRouteId";
import { brainScreenSurface } from "../../services/connectionLifecycle";
import { isTargetedBrainThreadReadOnly } from "../../services/brainThreadRouting";
import { useWorkers, type ConnectionState } from "../../store/workers";
import {
  useBrain,
  type BrainExecutorRef,
} from "../../store/brain";
import type { BrainWorkResultEvent } from "../../components/brain/brainWorkEvent";
import { useCurrentServer } from "../../store/currentServer";
import {
  BrainCompanionContext,
  type BrainCompanion,
} from "../../components/mewla/BrainCompanion";
import { brainCatTap, presenceWorkGroup, resolveBrainCatPresence } from "../../components/mewla/brainCatState";
import {
  brainWorkActions,
  brainWorkAskDraft,
  brainWorkSnoozeUntil,
  brainWorkUserAction,
  type BrainWorkAction,
} from "../../components/brain/brainWorkActions";
import type { WorkSlipAction } from "../../components/brain/WorkSlip";
import { confirmDestructive } from "../../components/ui/confirmDestructive";
import { useToast } from "../../components/ui/Toast";
import { BrainStatusState } from "../../components/mewla/BrainStatusState";

const BRAIN_EMPTY_TITLE = "Ready when you are";
const BRAIN_EMPTY_BODY =
  "Brain naps in the seal until you ask, then gets to work and brings back what it finds.";

export default function BrainScreen() {
  const router = useRouter();
  const params = useLocalSearchParams<{
    brainThreadId?: string;
    brainMessageId?: string;
    serverId?: string;
    /** Desktop web palette: start a new chat, or open one Work. */
    newChat?: string;
    work?: string;
  }>();
  const { theme: appTheme } = useAppTheme();
  const styles = useMemo(() => createStyles(), []);
  const insets = useSafeAreaInsets();
  const topChromeInset = resolvePrimaryAppBarGeometry(insets.top).contentInset;
  const { chrome, theme } = useMemo(
    () => buildChatChrome(appTheme),
    [appTheme],
  );
  const { state: workerState } = useWorkers();
  const { state: brainState } = useBrain();
  const {
    currentServer: activeServer,
    hydrated: currentServerHydrated,
    switchCurrentServer,
    isCurrentServer,
  } = useCurrentServer();
  const screenFocused = useIsFocused();
  const [adapterSheetVisible, setAdapterSheetVisible] = useState(false);
  const [menuVisible, setMenuVisible] = useState(false);
  const [switchingAdapterId, setSwitchingAdapterId] = useState<string | null>(
    null,
  );
  const [adapterSwitchError, setAdapterSwitchError] = useState<string | null>(
    null,
  );
  const [newChatLoading, setNewChatLoading] = useState(false);
  // Action failures say so at the action (a toast), never as a banner over the chat.
  const toast = useToast();
  const [workspaceViewerVisible, setWorkspaceViewerVisible] = useState(false);

  const routeServerId = params.serverId?.trim() || "";
  const routeServerMatches =
    !routeServerId || activeServer?.id === routeServerId;

  useEffect(() => {
    if (
      !currentServerHydrated ||
      !routeServerId ||
      activeServer?.id === routeServerId
    ) {
      return;
    }
    void switchCurrentServer(routeServerId).catch((error) => {
      toast.show({
        title: "Couldn't switch servers",
        detail: error instanceof Error ? error.message : "Unable to switch to the requested server.",
        tone: "error",
      });
    });
  }, [
    activeServer?.id,
    currentServerHydrated,
    routeServerId,
    switchCurrentServer,
    toast,
  ]);

  const activeBrain = activeServer && routeServerMatches
    ? brainState.byServer[activeServer.id]
    : null;
  const connectionState: ConnectionState = activeServer
    ? workerState.serverConnections[activeServer.id] || "offline"
    : "offline";
  const connectionIssue = activeServer
    ? (workerState.serverConnectionIssues[activeServer.id] ?? null)
    : null;
  const hostWorker = activeBrain?.host_worker ?? null;
  const hostWorkerId = hostWorker?.id;
  const hostWorkerProcessId = hostWorker?.process_id;
  const hostWorkerStartedAt = hostWorker?.started_at;
  const brainHostFileSession = useMemo(
    () =>
      hostWorkerId && hostWorkerProcessId && hostWorkerStartedAt
        ? {
            workerId: hostWorkerId,
            processId: hostWorkerProcessId,
            startedAt: hostWorkerStartedAt,
          }
        : null,
    [hostWorkerId, hostWorkerProcessId, hostWorkerStartedAt],
  );
  const hostExecutor = activeBrain?.host_executor ?? null;
  const routedThreadId = routeServerMatches ? params.brainThreadId : undefined;
  const displayedThreadId = routedThreadId || activeBrain?.chat_thread_id;
  const targetedThreadReadOnly = isTargetedBrainThreadReadOnly(
    routedThreadId,
    activeBrain?.chat_thread_id,
  );
  const brainChatScopeKey = displayedThreadId
    ? `brain-thread:${displayedThreadId}`
    : undefined;

  const ready = Boolean(activeServer && activeBrain?.hydrated && hostWorker?.id);
  const { width: windowWidth } = useWindowDimensions();
  // Wide screens get the Work column; it holds the cat and Brain's presence.
  const workColumn = windowWidth >= PRIMARY_SIDEBAR_BREAKPOINT;
  const workSurface = useMemo(
    () => brainWorkSurface(activeBrain?.current_work, activeBrain?.workers),
    [activeBrain?.current_work, activeBrain?.workers],
  );
  const [workSheetVisible, setWorkSheetVisible] = useState(false);
  const [workReveal, setWorkReveal] = useState<BrainWorkReveal | null>(null);
  const [selectedWorkSlip, setSelectedWorkSlip] = useState<BrainWorkSlip | null>(null);
  const [brainTurnRunning, setBrainTurnRunning] = useState(false);
  useEffect(() => {
    setWorkSheetVisible(false);
    setWorkReveal(null);
    setSelectedWorkSlip(null);
  }, [activeServer?.id]);
  const openWorkList = useCallback(() => setWorkSheetVisible(true), []);
  const sessionLabels = useMemo(() => {
    const labels = new Map<string, string>();
    for (const worker of activeBrain?.workers ?? []) {
      const label = workerWho(worker);
      if (label) labels.set(worker.id, label);
    }
    return labels;
  }, [activeBrain?.workers]);
  const [workActionBusy, setWorkActionBusy] = useState<string | null>(null);
  const [replyOpenFor, setReplyOpenFor] = useState<string | null>(null);
  // "Ask Brain about this": the composer's draft setter, seeded once.
  const composerDraftRef = useRef<((value: string) => void) | null>(null);
  const [pendingAskDraft, setPendingAskDraft] = useState<string | null>(null);
  const retryConnection = useCallback(() => {
    if (!activeServer || !isCurrentServer(activeServer.id)) return;
    void setServerAutoConnect(activeServer.id, true).then(() => {
      if (isCurrentServer(activeServer.id)) wsClient.connect(activeServer);
    }).catch((error) => toast.show({ title: "Couldn't reconnect", detail: String(error), tone: "error" }));
  }, [activeServer, isCurrentServer, toast]);
  const openPairing = useCallback(() => {
    router.push({ pathname: "/settings", params: activeServer ? {} : { addServer: Date.now().toString() } });
  }, [activeServer, router]);
  const brainSurface = brainScreenSurface({
    hasServer: Boolean(activeServer),
    connection: connectionState,
    hydrated: Boolean(activeBrain?.hydrated),
    hasHostWorker: Boolean(hostWorker?.id),
    structuredEvents: Boolean(hostExecutor?.capabilities?.structured_events),
  });
  // Until Brain's first snapshot the chat shows its loading outline and the
  // composer waits, saying "Connecting…".
  const brainWaking = brainSurface === "waking";
  const brainModelSheet = useSessionProviderSheet({
    serverId: activeServer?.id ?? "",
    workerId: hostWorker?.id ?? "",
    capabilities: hostWorker?.capabilities ?? null,
    connectionConnected: connectionState === "connected",
    eagerLoad: true,
    focusActive: screenFocused,
  });
  const canUseStructuredBrainInterface = ready && brainSurface === "chat";
  const availableExecutors = activeBrain?.executors ?? [];
  const canSwitchAdapter = availableExecutors.length > 1;
  const openAdapterSheet = useCallback(() => {
    if (!canSwitchAdapter || !activeServer) {
      return;
    }
    setAdapterSwitchError(null);
    setAdapterSheetVisible(true);
  }, [activeServer, canSwitchAdapter]);

  const closeAdapterSheet = useCallback(() => {
    setAdapterSheetVisible(false);
    setAdapterSwitchError(null);
  }, []);

  const openMenu = useCallback(() => {
    setMenuVisible(true);
  }, []);

  const closeMenu = useCallback(() => {
    setMenuVisible(false);
  }, []);

  const openWorkspaceViewer = useCallback(() => {
    if (!activeServer) {
      return;
    }
    setWorkspaceViewerVisible(true);
  }, [activeServer]);

  const closeWorkspaceViewer = useCallback(() => {
    setWorkspaceViewerVisible(false);
  }, []);

  const openBrainTerminal = useCallback(() => {
    if (!activeServer || !hostWorker?.id) {
      return;
    }
    router.push({
      pathname: "/terminal/[id]",
      params: terminalRouteParams(hostWorker.id, activeServer.id, {
        initialInterfaceRenderMode: "terminal",
      }),
    });
  }, [activeServer, hostWorker?.id, router]);

  const startNewBrainChat = useCallback(async () => {
    if (!activeServer || !activeBrain?.hydrated || newChatLoading) {
      return;
    }
    setNewChatLoading(true);
    try {
      await wsClient.startNewBrainChat(activeServer.id);
    } catch (error: any) {
      toast.show({
        title: "Couldn't start a new chat",
        detail: error?.message || "Failed to start a new Brain chat.",
        tone: "error",
      });
    } finally {
      setNewChatLoading(false);
    }
  }, [activeBrain?.hydrated, activeServer, newChatLoading, toast]);

  const switchExecutor = useCallback(
    async (adapter: BrainExecutorRef) => {
      if (!activeServer || !adapter.id || switchingAdapterId) {
        return;
      }
      if (adapter.id === hostExecutor?.id) {
        closeAdapterSheet();
        return;
      }
      setSwitchingAdapterId(adapter.id);
      setAdapterSwitchError(null);
      try {
        await wsClient.setBrainExecutor(activeServer.id, adapter.id);
        closeAdapterSheet();
      } catch (error: any) {
        setAdapterSwitchError(error?.message || "Failed to switch executor.");
      } finally {
        setSwitchingAdapterId(null);
      }
    },
    [
      activeServer,
      closeAdapterSheet,
      hostExecutor?.id,
      switchingAdapterId,
    ],
  );

  const canNewChat = Boolean(activeServer && activeBrain?.hydrated);
  const canOpenTerminal = Boolean(activeServer && hostWorker?.id);
  const canOpenWorkspace = Boolean(
    activeServer && connectionState === "connected",
  );
  const canOpenWorkList = !workColumn && canUseStructuredBrainInterface;
  const overflowDisabled =
    !canOpenWorkList && !canNewChat && !canOpenTerminal && !canOpenWorkspace && !canSwitchAdapter;

  const brainPageAction = useMemo(
    () => ({
      accessibilityLabel: "Brain actions",
      disabled: overflowDisabled,
      onPress: openMenu,
    }),
    [openMenu, overflowDisabled],
  );
  usePrimaryPageAction(brainPageAction);

  const menuActions = useMemo(
    () => [
      // Phone: the Work list opens from the cat's tail row, which carries
      // the count ("3 Workers running"). This is the way in when there is no
      // tail row: during a turn, with the cat perched on a slip, or in an
      // empty chat. Wide screens keep the column.
      ...(canOpenWorkList
        ? [
            {
              key: "work",
              label: "Work",
              icon: "layers" as const,
              onPress: openWorkList,
            },
          ]
        : []),
      {
        key: "new-chat",
        label: "New chat",
        icon: newChatLoading
          ? ("hourglass" as const)
          : ("edit" as const),
        disabled: !canNewChat || newChatLoading,
        onPress: () => {
          void startNewBrainChat();
        },
      },
      ...(canSwitchAdapter
        ? [
            {
              key: "executor",
              label: "Switch executor",
              accessibilityLabel:
                switchExecutorAccessibilityLabel(hostExecutor),
              icon: "swap-horizontal" as const,
              trailing: hostExecutor?.id ? (
                <BrainExecutorIcon adapter={hostExecutor} size={14} />
              ) : undefined,
              onPress: openAdapterSheet,
            },
          ]
        : []),
      {
        key: "terminal",
        label: "Open terminal",
        icon: "terminal" as const,
        disabled: !canOpenTerminal,
        onPress: openBrainTerminal,
      },
      {
        key: "workspace",
        label: "Browse workspace",
        icon: "folder-open" as const,
        disabled: !canOpenWorkspace,
        onPress: openWorkspaceViewer,
      },
    ],
    [
      canNewChat,
      canOpenTerminal,
      canOpenWorkspace,
      canSwitchAdapter,
      canOpenWorkList,
      hostExecutor,
      newChatLoading,
      openAdapterSheet,
      openBrainTerminal,
      openWorkList,
      openWorkspaceViewer,
      startNewBrainChat,
    ],
  );

  const renderBrainComposerAccessory = useCallback(
    ({
      draft,
      setDraft,
    }: {
      draft: string;
      setDraft: (value: string) => void;
    }) => {
      composerDraftRef.current = setDraft;
      const activeMention = activeExecutorMentionAtEnd(draft);
      if (!activeMention || availableExecutors.length === 0) {
        return null;
      }
      return (
        <BrainExecutorMentionPicker
          executors={availableExecutors}
          activeAdapterId={hostExecutor?.id}
          query={activeMention.query}
          chrome={chrome}
          onSelect={(adapter) => {
            const before = draft.slice(0, activeMention.start);
            const next = `${before}@${adapter.id} `;
            setDraft(next);
          }}
        />
      );
    },
    [availableExecutors, chrome, hostExecutor?.id],
  );

  const [selectedWorkResult, setSelectedWorkResult] = useState<{ event: BrainWorkResultEvent; serverId: string } | null>(null);
  useEffect(() => setSelectedWorkResult(null), [activeServer?.id, brainChatScopeKey]);
  const activateWorkResult = useCallback(
    (event: BrainWorkResultEvent) => {
      if (!activeServer) {
        return;
      }
      if (event.unread) {
        wsClient.markBrainWorkRead(activeServer.id, event.work_id);
      }
      setSelectedWorkResult({ event, serverId: activeServer.id });
    },
    [activeServer],
  );
  const openSessionIds = useMemo(
    () => new Set((activeBrain?.workers ?? []).map((agent) => agent.id)),
    [activeBrain?.workers],
  );
  const detailEvent = selectedWorkResult?.serverId === activeServer?.id ? selectedWorkResult?.event ?? null : null;
  const openWorkerSession = useCallback(
    (sessionId: string) => {
      if (!activeServer) return;
      setWorkSheetVisible(false);
      setSelectedWorkSlip(null);
      router.push({ pathname: "/terminal/[id]", params: terminalRouteParams(sessionId, activeServer.id) });
    },
    [activeServer, router],
  );
  const openWorkSlip = useCallback(
    (slip: BrainWorkSlip) => {
      setWorkSheetVisible(false);
      if (slip.unread && activeServer) {
        wsClient.markBrainWorkRead(activeServer.id, slip.workId);
      }
      // A running Worker opens its Session; everything else opens the Work.
      if (slip.sessionId && slip.group === "running" && !slip.replied) {
        openWorkerSession(slip.sessionId);
        return;
      }
      setReplyOpenFor(null);
      setSelectedWorkSlip(slip);
    },
    [activeServer, openWorkerSession],
  );
  // The desktop palette asks through the URL; each request runs once.
  // A request that can't run now is dropped, never replayed later.
  useEffect(() => {
    if (!params.newChat) return;
    router.setParams({ newChat: undefined });
    if (canNewChat) void startNewBrainChat();
  }, [canNewChat, params.newChat, router, startNewBrainChat]);
  useEffect(() => {
    if (!params.work) return;
    router.setParams({ work: undefined });
    const slip = workSurface.slips.find((candidate) => candidate.workId === params.work);
    if (slip) openWorkSlip(slip);
  }, [openWorkSlip, params.work, router, workSurface.slips]);
  useEffect(() => {
    if (pendingAskDraft === null || !composerDraftRef.current) return;
    composerDraftRef.current(pendingAskDraft);
    setPendingAskDraft(null);
  }, [pendingAskDraft]);
  const sendWorkAction = useCallback(
    async (slip: BrainWorkSlip, action: BrainWorkAction, text?: string) => {
      const kind = brainWorkUserAction(action.kind);
      if (!activeServer || !kind) return;
      const busyKey = `${slip.workId}:${action.kind}:${action.text ?? ""}`;
      setWorkActionBusy(busyKey);
      try {
        const result = await wsClient.actOnBrainWork(activeServer.id, slip.workId, kind, {
          text: text ?? action.text,
          snoozeUntil: kind === "snooze" ? brainWorkSnoozeUntil().toISOString() : undefined,
        });
        setSelectedWorkSlip(null);
        setReplyOpenFor(null);
        if (kind === "reply") {
          toast.show(
            result.admission === "uncertain"
              ? { title: "Sent, but Brain may not have it", detail: "Check Brain's chat before sending again.", tone: "info" }
              : { title: "Brain has your answer", tone: "success" },
          );
        } else if (kind === "snooze") {
          toast.show({ title: "Snoozed until tomorrow morning", tone: "info" });
        } else {
          toast.show({ title: kind === "close" ? "Closed" : kind === "stop" ? "Stopped" : "Dismissed", tone: "success" });
        }
      } catch (error) {
        toast.show({ title: "That didn't go through", detail: error instanceof Error ? error.message : String(error), tone: "error" });
      } finally {
        setWorkActionBusy(null);
      }
    },
    [activeServer, toast],
  );
  const runWorkAction = useCallback(
    (slip: BrainWorkSlip, action: BrainWorkAction) => {
      switch (action.kind) {
        case "open":
          if (slip.sessionId) openWorkerSession(slip.sessionId);
          return;
        case "reply":
          setWorkSheetVisible(false);
          setReplyOpenFor(slip.workId);
          setSelectedWorkSlip(slip);
          return;
        case "ask":
          setWorkSheetVisible(false);
          setSelectedWorkSlip(null);
          setPendingAskDraft(brainWorkAskDraft(slip));
          return;
        case "read":
          if (activeServer) wsClient.markBrainWorkRead(activeServer.id, slip.workId);
          setSelectedWorkSlip(null);
          return;
      }
      if (action.confirm) {
        confirmDestructive({
          title: action.kind === "stop" ? `Stop “${slip.title}”?` : `${action.label}?`,
          message: action.confirm,
          confirmLabel: action.label,
          onConfirm: () => sendWorkAction(slip, action),
        });
        return;
      }
      void sendWorkAction(slip, action);
    },
    [activeServer, openWorkerSession, sendWorkAction],
  );
  const workActionsFor = useCallback(
    (slip: BrainWorkSlip, placement: "slip" | "sheet"): WorkSlipAction[] => {
      const all = brainWorkActions(slip);
      // On the slip: the answers (or the primary step) and nothing destructive.
      const shown = placement === "sheet"
        ? all.filter((action) => !(action.kind === "reply" && slip.closed))
        : slip.question
          ? all.filter((action) => action.kind === "answer" || action.kind === "reply").slice(0, 3)
          : brainWorkSlipStale(slip)
            // Waiting for days: offer the way out beside the reply.
            ? all.filter((action) => action.primary || action.kind === "dismiss").slice(0, 2)
            : all.filter((action) => action.primary).slice(0, 1);
      return shown.map((action) => ({
        key: `${action.kind}:${action.text ?? action.label}`,
        label: action.label,
        primary: action.primary,
        busy: workActionBusy === `${slip.workId}:${action.kind}:${action.text ?? ""}`,
        disabled: Boolean(workActionBusy && workActionBusy.startsWith(`${slip.workId}:`)),
        onPress: () => runWorkAction(slip, action),
      }));
    },
    [runWorkAction, workActionBusy],
  );
  const brainCompanion = useMemo<BrainCompanion>(() => {
    const presence = resolveBrainCatPresence({
      hasServer: Boolean(activeServer),
      connection: connectionState,
      hydrated: Boolean(activeBrain?.hydrated),
      currentWork: activeBrain?.current_work,
    });
    return {
      // The Work column perches the cat on its first Needs-you slip; the
      // conversation then has no tail row, so there is one cat on screen.
      presence: workColumn && presence.state === "attention" ? { ...presence, away: true } : presence,
      animate: screenFocused,
      sessionLabels,
      // The tail row's count is the Work list's: on a phone it opens the
      // sheet, beside the column it scrolls to the group it counts.
      onOpenWork: workColumn
        ? () => {
            const group = presenceWorkGroup(presence) ?? BRAIN_WORK_GROUP_ORDER.find((item) => workSurface.counts[item]);
            if (group) setWorkReveal((previous) => ({ group, seq: (previous?.seq ?? 0) + 1 }));
          }
        : openWorkList,
      // A Work result in the conversation is that Work's slip: same actions,
      // same sheet.
      workSlip: (workId) => {
        const slip = workSurface.slips.find((item) => item.workId === workId);
        return slip ? { slip, actions: workActionsFor(slip, "slip") } : undefined;
      },
      onTurnRunning: setBrainTurnRunning,
      onCatTap: ({ turnRunning, turnLabel }) => {
        const answer = brainCatTap({ presence, turnRunning, turnLabel, counts: workSurface.counts });
        switch (answer.kind) {
          case "retry":
            retryConnection();
            return "Knocking on your computer…";
          case "pair":
            openPairing();
            return null;
          case "open-work": {
            const slip = workSurface.slips.find((item) => item.workId === answer.workId);
            if (slip) setSelectedWorkSlip(slip);
            return null;
          }
          default:
            return answer.text;
        }
      },
    };
  }, [
    activeBrain?.current_work,
    activeBrain?.hydrated,
    activeServer,
    connectionState,
    openPairing,
    openWorkList,
    retryConnection,
    screenFocused,
    sessionLabels,
    workActionsFor,
    workColumn,
    workSurface,
  ]);
  // The open sheet follows the Work as the daemon updates it.
  const liveSelectedSlip = selectedWorkSlip
    ? workSurface.slips.find((slip) => slip.workId === selectedWorkSlip.workId) ?? selectedWorkSlip
    : null;
  return (
    <SafeAreaView
      style={[styles.screen, { backgroundColor: chrome.appBackground }]}
      edges={[]}
    >
      <BrainWorkEventDetailSheet
        event={detailEvent}
        chrome={chrome}
        onClose={() => setSelectedWorkResult(null)}
        onOpenSession={detailEvent?.session_id && openSessionIds.has(detailEvent.session_id) && activeServer ? () => {
          const id = detailEvent.session_id!;
          setSelectedWorkResult(null);
          router.push({ pathname: "/terminal/[id]", params: terminalRouteParams(id, activeServer.id) });
        } : undefined}
      />
      {targetedThreadReadOnly ? (
        <View style={[styles.notices, { paddingTop: topChromeInset }]}>
          <InlineNotice
            icon="lock"
            title="Historical Brain thread"
            detail="Read-only. Start a new chat from the Brain menu."
          />
        </View>
      ) : null}

      <View style={styles.row}>
      <View style={styles.surface}>
        <ChatCanvas chrome={chrome}>
          <BrainCompanionContext.Provider value={brainCompanion}>
            {canUseStructuredBrainInterface || brainWaking ? (
              <InterfaceChatSurface
                key={`brain-chat:${activeServer?.id}:${brainChatScopeKey ?? ""}`}
                visible
                serverId={activeServer?.id ?? ""}
                serverUrl={activeServer?.url ?? ""}
                daemonId={activeServer?.daemonId ?? ""}
                workerId={hostWorker?.id ?? ""}
                conversationScopeKey={brainChatScopeKey}
                workerInfo={{
                  cwd: hostWorker?.cwd,
                  command: hostWorker?.command,
                  name: hostWorker?.name,
                  processId: hostWorker?.process_id,
                  startedAt: hostWorker?.started_at,
                }}
                connectionState={brainWaking ? "connecting" : connectionState}
                connectionIssue={connectionIssue}
                theme={theme}
                chrome={chrome}
                screenFocused={screenFocused}
                topChromeInset={topChromeInset}
                onBrainWorkEventActivate={
                  targetedThreadReadOnly ? undefined : activateWorkResult
                }
                brainCurrentWork={activeBrain?.current_work}
                openSessionIds={openSessionIds}
                readOnly={targetedThreadReadOnly}
                onSwitchToTerminal={openBrainTerminal}
                // Brain is who you talk to; the host executor is an implementation detail.
                placeholder={connectionState === "connected" && !brainWaking ? "Tell Brain…" : undefined}
                emptyTitle={BRAIN_EMPTY_TITLE}
                emptyBody={BRAIN_EMPTY_BODY}
                renderComposerAccessory={brainWaking ? undefined : renderBrainComposerAccessory}
                composerModelControl={brainWaking ? null : brainModelSheet.composerControl}
                onComposerModelControlPress={() => brainModelSheet.open()}
              />
            ) : brainSurface === "status" ? (
              <BrainStatusState
                hasServer={Boolean(activeServer)}
                animate={screenFocused}
                connected={
                  connectionState === "connected" ||
                  connectionState === "connecting"
                }
                onSettings={() => router.push({ pathname: "/settings", params: activeServer ? {} : { addServer: Date.now().toString() } })}
                onRetry={retryConnection}
              />
            ) : (
              <BrainInterfaceUnavailableState
                provider={hostExecutor?.provider}
                onOpenTerminal={canOpenTerminal ? openBrainTerminal : undefined}
                onSwitchExecutor={canSwitchAdapter ? openAdapterSheet : undefined}
              />
            )}
          </BrainCompanionContext.Provider>
        </ChatCanvas>
      </View>
      {workColumn && canUseStructuredBrainInterface ? (
        <BrainWorkColumn
          surface={workSurface}
          objective={activeBrain?.objective}
          chrome={chrome}
          topInset={topChromeInset}
          animate={screenFocused}
          perch={!brainTurnRunning}
          reveal={workReveal}
          onOpenSlip={openWorkSlip}
          actionsFor={workActionsFor}
          onCatPress={() => {
            const first = workSurface.slips.find((slip) => slip.group === "needs");
            if (first) openWorkSlip(first);
          }}
        />
      ) : null}
      </View>

      <BrainWorkSheet
        visible={workSheetVisible && !workColumn}
        surface={workSurface}
        objective={activeBrain?.objective}
        chrome={chrome}
        onClose={() => setWorkSheetVisible(false)}
        onOpenSlip={openWorkSlip}
        actionsFor={workActionsFor}
      />
      <BrainWorkDetailSheet
        slip={liveSelectedSlip}
        waitFor={activeBrain?.current_work?.find((work) => work.work_id === selectedWorkSlip?.workId)?.wait_for}
        chrome={chrome}
        actions={liveSelectedSlip ? workActionsFor(liveSelectedSlip, "sheet") : undefined}
        replyOpen={replyOpenFor === liveSelectedSlip?.workId}
        replyBusy={Boolean(liveSelectedSlip && workActionBusy?.startsWith(`${liveSelectedSlip.workId}:reply`))}
        onReply={(text) => {
          if (liveSelectedSlip) void sendWorkAction(liveSelectedSlip, { kind: "reply", label: "Reply" }, text);
        }}
        onClose={() => {
          setSelectedWorkSlip(null);
          setReplyOpenFor(null);
        }}
      />

      <BrainExecutorSheet
        visible={adapterSheetVisible}
        executors={availableExecutors}
        hostAdapterId={hostExecutor?.id}
        switchingAdapterId={switchingAdapterId}
        error={adapterSwitchError}
        onClose={closeAdapterSheet}
        onSelect={(adapter) => void switchExecutor(adapter)}
      />

      <ActionMenu
        visible={menuVisible}
        title="Brain"
        items={menuActions}
        onClose={closeMenu}
      />

      <SessionModelSheet
        visible={brainModelSheet.visible}
        loading={brainModelSheet.loading}
        activating={brainModelSheet.activating}
        error={brainModelSheet.error}
        rows={brainModelSheet.rows}
        chrome={chrome}
        onClose={brainModelSheet.close}
        onRetry={brainModelSheet.retry}
        onActivate={(choice) => {
          void brainModelSheet.activate(choice);
        }}
      />

      <BrainWorkspaceViewer
        visible={workspaceViewerVisible}
        serverId={activeServer?.id}
        daemonId={activeServer?.daemonId}
        workspace={activeBrain?.workspace}
        hostSession={brainHostFileSession}
        chrome={chrome}
        theme={theme}
        onClose={closeWorkspaceViewer}
      />
    </SafeAreaView>
  );
}

function BrainInterfaceUnavailableState({
  provider,
  onOpenTerminal,
  onSwitchExecutor,
}: {
  provider?: string;
  onOpenTerminal?: () => void;
  onSwitchExecutor?: () => void;
}) {
  const label = brainProviderLabel(provider);
  return (
    <ScrollView contentContainerStyle={{ flexGrow: 1, justifyContent: "center" }}>
      <EmptyState
        icon="layers"
        title="Chat view unavailable"
        detail={
          label
            ? `${label} is connected, but this executor does not expose structured chat events.`
            : "Switch the Brain host executor to one with structured chat events."
        }
        action={onSwitchExecutor ? { label: "Switch executor", icon: "swap-horizontal", onPress: onSwitchExecutor } : undefined}
        secondary={onOpenTerminal ? { label: "Open terminal", icon: "terminal", onPress: onOpenTerminal } : undefined}
      />
    </ScrollView>
  );
}

function activeExecutorMentionAtEnd(
  value: string,
): { start: number; query: string } | null {
  const end = value.length;
  let cursor = end - 1;
  while (cursor >= 0) {
    const char = value[cursor];
    if (char === "@") {
      if (cursor === 0 || /\s/.test(value[cursor - 1])) {
        return {
          start: cursor,
          query: value.slice(cursor + 1, end),
        };
      }
      return null;
    }
    if (!/[a-z0-9_.-]/i.test(char)) {
      return null;
    }
    cursor -= 1;
  }
  return null;
}

function createStyles() {
  return StyleSheet.create({
    screen: {
      flex: 1,
    },
    row: {
      flex: 1,
      flexDirection: "row",
    },
    surface: {
      flex: 1,
    },
    notices: {
      gap: 6,
      paddingHorizontal: CHAT_CHROME_HORIZONTAL_INSET,
      paddingBottom: 6,
      zIndex: 2,
    },
  });
}
