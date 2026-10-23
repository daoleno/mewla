import React, {
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { latestUserMessageAt } from "./latestUserMessageAt";
import { AppState } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { useAppTheme } from "../../constants/tokens";
import {
  isWorkerSessionListFreshForConnection,
  useWorkers,
  type ConnectionState,
} from "../../store/workers";
import { agentKindFromProvider } from "../../services/workerPresentation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import { wsClient } from "../../services/websocket";
import type { InterfaceChatBodyProps } from "./InterfaceChatBody";
import { useInterfaceChatController } from "./InterfaceChatController";
import {
  type InterfaceChatWorkerInfo,
  type PendingUserMessage,
  useInterfaceChatSession,
} from "./InterfaceChatSession";
import { CodexStatusSheet } from "./CodexStatusSheet";
import { useCodexStatus } from "./useCodexStatus";
import { CodexSkillsSheet } from "./CodexSkillsSheet";
import {
  buildTerminalActionPrompt,
  type TerminalActionPromptOption,
} from "./TerminalActionPromptModel";
import {
  latestPendingChoice,
  type ChoiceAnswerPayload,
  type InterfaceChoicePrompt,
} from "./claudeChoiceModel";
import { liveActionPromptScopeKey } from "../../services/workerSessionListTransport";
import { useCodexSlashCommands } from "./CodexSlashCommands";
import { useInterfaceChatBodyProps } from "./useInterfaceChatBodyProps";
import { finalReplyIdsOf } from "./InterfaceTimelineModel";
import { useInterfaceMessageActions } from "./useInterfaceMessageActions";
import { useDesktopWeb } from "../navigation/useDesktopWeb";
import {
  useInterfaceComposerInput,
  useInterfaceComposerPresentation,
  usePinnedTimeline,
  useRelativeTimeLabel,
} from "./InterfaceChatSurfaceHooks";
import { resolveRunningProviderActivity } from "./providerActivity";
import { resolveInterfaceReadingIdentity, type InterfaceReadingIdentity } from "./interfaceChatSessionIdentity";
import { shouldClearTurnFocusForSurfaceLifecycle } from "./turnFocusState";
import {
  resolveInterfaceComposerInitialFocusEffect,
  type InterfaceComposerInitialFocusGrant,
} from "./interfaceComposerInitialFocus";
import { makeSessionKey } from "../../services/sessionKeys";

interface UseInterfaceChatSurfaceStateInput {
  visible: boolean;
  serverId: string;
  serverUrl: string;
  daemonId: string;
  workerId: string;
  conversationScopeKey?: string;
  workerInfo?: InterfaceChatWorkerInfo;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  initialComposerFocusGrant?: InterfaceComposerInitialFocusGrant;
  placeholder?: string;
  keyboardVerticalOffset?: number;
  topChromeInset?: number;
  showUnavailableAction?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
  onBrainWorkEventActivate?: (
    event: import("../brain/brainWorkEvent").BrainWorkResultEvent,
    canOpenSession: boolean,
  ) => void;
  brainCurrentWork?: readonly import("../../store/brain").BrainCurrentWork[];
  openSessionIds?: ReadonlySet<string>;
  composerAccessory?: ReactNode;
  onDraftChange?: (value: string) => void;
  renderComposerAccessory?: (args: {
    draft: string;
    setDraft: (value: string) => void;
    /** Sends text as if typed (live answers). */
    sendText: (text: string) => void;
  }) => ReactNode;
  /** Brain only: the answers to its last message, or none, as the chat stands. */
  resolveQuickReplies?: (args: {
    draft: string;
    /** Whether the composer has focus. */
    focused: boolean;
    /** When the person last wrote in this conversation, in ms; 0 if never. */
    lastUserMessageAt: number;
    /** Whether a message of the conversation is on screen to answer. */
    hasMessage: boolean;
  }) => readonly string[] | null;
  composerModelControl?:
    | import("../../services/providers/sessionModelHelpers").ComposerModelControlPresentation
    | null;
  onComposerModelControlPress?: () => void;
  onSwitchToTerminal?: () => void;
  onConsumeInitialComposerFocus?: () => void;
}

interface InterfaceChatSurfaceState {
  bodyProps: InterfaceChatBodyProps;
  messageActions: Pick<
    ReturnType<typeof useInterfaceMessageActions>,
    "environment" | "replyIds" | "onEdit"
  >;
}

const EMPTY_CONVERSATION_EVENTS: CodexConversationEvent[] = [];

export function useInterfaceChatSurfaceState({
  visible,
  serverId,
  serverUrl,
  daemonId,
  workerId,
  conversationScopeKey,
  workerInfo,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  initialComposerFocusGrant = null,
  placeholder,
  keyboardVerticalOffset,
  topChromeInset,
  showUnavailableAction,
  emptyTitle,
  emptyBody,
  onBrainWorkEventActivate,
  brainCurrentWork,
  openSessionIds,
  composerAccessory,
  onDraftChange,
  renderComposerAccessory,
  resolveQuickReplies,
  composerModelControl,
  onComposerModelControlPress,
  onSwitchToTerminal,
  onConsumeInitialComposerFocus,
}: UseInterfaceChatSurfaceStateInput): InterfaceChatSurfaceState {
  const insets = useSafeAreaInsets();
  const { theme: appTheme } = useAppTheme();
  const { state: workerState } = useWorkers();
  const composerLayout = appTheme.chat.layout;
  const active = visible && screenFocused;
  const handledInitialComposerFocusGrantRef =
    useRef<InterfaceComposerInitialFocusGrant>(null);
  const chatAgentKind = agentKindFromProvider(workerInfo?.provider);
  const connectionGeneration =
    workerState.connectionGenerationByServer[serverId] ?? 0;
  const workerSessionListFresh = isWorkerSessionListFreshForConnection(
    workerState,
    serverId,
  );
  const slashCommands = useCodexSlashCommands({
    serverId,
    connectionState,
    screenFocused: active,
    agentKind: chatAgentKind,
  });
  const [actionMenuPinned, setActionMenuPinned] = useState(false);
  const [skillsSheetVisible, setSkillsSheetVisible] = useState(false);
  const [statusSheetVisible, setStatusSheetVisible] = useState(false);
  const { state: statusState, load: loadStatus } = useCodexStatus({
    serverId,
    workerId,
    connected: connectionState === "connected",
  });
  const session = useInterfaceChatSession({
    serverId,
    workerId,
    conversationScopeKey,
    workerInfo,
    connectionState,
    screenFocused: active,
  });
  const {
    cacheKey: conversationCacheKey,
    conversation,
    loading,
    error,
    draft,
    setDraft,
    restoreDraft,
    attachments,
    setAttachments,
    pendingUserMessages,
    subscriptionGeneration,
    turnFocusAnchorAliases,
    addPendingUserMessage,
    beginPendingUserMessageAttempt,
    settlePendingUserMessage,
    discardPendingUserMessage,
  } = session;
  const setObservedDraft = useCallback(
    (value: string) => {
      setDraft(value);
      onDraftChange?.(value);
    },
    [onDraftChange, setDraft],
  );
  const restoreObservedDraft = useCallback(
    (value: string) => {
      restoreDraft(value);
      onDraftChange?.(value);
    },
    [onDraftChange, restoreDraft],
  );
  const sendTextRef = useRef<(text: string) => void>(() => undefined);
  const sendQuickText = useCallback((text: string) => sendTextRef.current(text), []);
  const lastUserMessageAt = latestUserMessageAt(
    conversation?.events,
    pendingUserMessages,
  );
  const renderedComposerAccessory = renderComposerAccessory
    ? renderComposerAccessory({
        draft,
        setDraft: setObservedDraft,
        sendText: sendQuickText,
      })
    : composerAccessory;
  const composerInput = useInterfaceComposerInput({
    enabled: active && connectionState === "connected",
  });
  const quickReplyChips = resolveQuickReplies?.({
    draft,
    focused: composerInput.focused,
    lastUserMessageAt,
    hasMessage: Boolean(
      conversation?.events.some((event) => event.kind === "assistant_message"),
    ),
  });
  const quickReplies = useMemo(
    () => (quickReplyChips ? { chips: quickReplyChips, onSelect: sendQuickText } : undefined),
    [quickReplyChips, sendQuickText],
  );
  useEffect(() => {
    const routeSessionKey = makeSessionKey(serverId, workerId);
    const effect = resolveInterfaceComposerInitialFocusEffect({
      grant: initialComposerFocusGrant,
      handledGrant: handledInitialComposerFocusGrantRef.current,
      sessionKey: routeSessionKey,
      screenActive: active,
      appActive: AppState.currentState === "active",
      connected: connectionState === "connected",
    });
    if (effect === "ignore" || effect === "wait") {
      return;
    }
    handledInitialComposerFocusGrantRef.current = initialComposerFocusGrant;
    onConsumeInitialComposerFocus?.();
    if (effect === "deliver") {
      composerInput.focus();
    }
  }, [
    active,
    workerId,
    composerInput.focus,
    connectionState,
    initialComposerFocusGrant,
    onConsumeInitialComposerFocus,
    serverId,
  ]);
  useEffect(() => {
    if ((!visible || connectionState !== "connected") && actionMenuPinned) {
      setActionMenuPinned(false);
    }
  }, [actionMenuPinned, connectionState, visible]);

  const toggleActionMenu = useCallback(() => {
    if (connectionState !== "connected") {
      composerInput.focus();
      return;
    }
    if (!actionMenuPinned) {
      composerInput.blur();
    }
    setActionMenuPinned(!actionMenuPinned);
  }, [
    actionMenuPinned,
    composerInput.blur,
    composerInput.focus,
    connectionState,
  ]);

  const dismissActionMenu = useCallback(() => {
    setActionMenuPinned(false);
  }, []);
  const handleModelControlPress = useCallback(() => {
    composerInput.blur();
    onComposerModelControlPress?.();
  }, [composerInput.blur, onComposerModelControlPress]);
  const openSkillsSheet = useCallback(() => {
    setActionMenuPinned(false);
    setSkillsSheetVisible(true);
  }, []);
  const closeSkillsSheet = useCallback(() => {
    setSkillsSheetVisible(false);
  }, []);
  const events = conversation?.events ?? EMPTY_CONVERSATION_EVENTS;
  const openStatusSheet = useCallback(() => {
    setActionMenuPinned(false);
    setStatusSheetVisible(true);
    loadStatus();
  }, [loadStatus]);
  const closeStatusSheet = useCallback(() => {
    setStatusSheetVisible(false);
  }, []);
  const switchFromStatusToTerminal = useCallback(() => {
    if (!onSwitchToTerminal) {
      return;
    }
    setStatusSheetVisible(false);
    onSwitchToTerminal();
  }, [onSwitchToTerminal]);
  const latestTimelineTimestamp = useMemo(
    () => latestChatTimelineTimestamp(conversation, pendingUserMessages),
    [conversation, pendingUserMessages],
  );
  const jumpLabel = useRelativeTimeLabel(latestTimelineTimestamp);
  const runningActivity = useMemo(
    () => resolveRunningProviderActivity(conversation?.activity),
    [conversation?.activity],
  );
  const readingIdentityRef = useRef<InterfaceReadingIdentity | undefined>(undefined);
  const readingIdentity = resolveInterfaceReadingIdentity(
    conversationCacheKey,
    Boolean(conversationScopeKey),
    conversation?.available ? conversation.session_id : undefined,
    readingIdentityRef.current,
  );
  readingIdentityRef.current = readingIdentity;
  const timeline = usePinnedTimeline(
    events.length + pendingUserMessages.length + (runningActivity ? 1 : 0),
    readingIdentity.key,
    topChromeInset,
  );
  const handleKeyboardLifecycleInvalidate = useCallback(
    (reason: "route" | "app" | "foreground_closed" | "ime_closed") => {
      if (reason !== "app") {
        composerInput.blur();
      }
      if (reason !== "ime_closed") {
        timeline.clearTurnFocusForLifecycle();
      }
      if (
        reason === "app" &&
        initialComposerFocusGrant &&
        handledInitialComposerFocusGrantRef.current !==
          initialComposerFocusGrant
      ) {
        handledInitialComposerFocusGrantRef.current = initialComposerFocusGrant;
        onConsumeInitialComposerFocus?.();
      }
    },
    [
      composerInput.blur,
      initialComposerFocusGrant,
      onConsumeInitialComposerFocus,
      timeline.clearTurnFocusForLifecycle,
    ],
  );
  useEffect(() => {
    if (
      shouldClearTurnFocusForSurfaceLifecycle(
        active && connectionState === "connected",
        subscriptionGeneration,
      )
    ) {
      timeline.clearTurnFocusForLifecycle();
    }
  }, [
    active,
    connectionState,
    subscriptionGeneration,
    timeline.clearTurnFocusForLifecycle,
  ]);
  const controller = useInterfaceChatController({
    serverId,
    workerId,
    conversationScopeKey,
    connectionState,
    connectionIssue,
    conversation,
    runningActivity,
    draft,
    setDraft: setObservedDraft,
    restoreDraft: restoreObservedDraft,
    attachments,
    pendingUserMessages,
    setAttachments,
    slashCommands,
    addPendingUserMessage,
    beginPendingUserMessageAttempt,
    settlePendingUserMessage,
    discardPendingUserMessage,
    requestTurnFocus: timeline.requestTurnFocus,
    focusComposer: composerInput.focus,
    clearComposerNativeText: composerInput.clearNativeText,
    dismissActionMenu,
    openStatusSheet,
    openSkillsSheet,
    onSwitchToTerminal,
  });
  sendTextRef.current = controller.sendText;

  const desktopWeb = useDesktopWeb();
  const replyIds = useMemo(() => finalReplyIdsOf(events), [events]);
  const messageActions = useInterfaceMessageActions({
    serverId,
    workerId,
    conversationScopeKey,
    connected: connectionState === "connected",
    running: Boolean(runningActivity),
    messageRewind:
      workerState.workers.find(
        (candidate) => candidate.key === makeSessionKey(serverId, workerId),
      )?.capabilities?.message_rewind === true,
    replyIds,
    desktop: desktopWeb,
    chrome,
    draft,
    setDraft: setObservedDraft,
    focusComposer: composerInput.focus,
    sendDraft: controller.sendDraft,
  });
  const sendEdited = messageActions.sendEdited;
  const editAwareController = useMemo(
    () => ({
      ...controller,
      sendDraft: () => void sendEdited(),
      canSend: controller.canSend && !messageActions.editBlocksSend,
    }),
    [controller, messageActions.editBlocksSend, sendEdited],
  );
  const composerPresentation = useInterfaceComposerPresentation({
    draft,
    slashCommands,
    workerProvider: workerInfo?.provider,
    connectionState,
    runningActivity,
    attachmentCount: attachments.length,
    interrupting: controller.interrupting,
    canSend: controller.canSend,
    elapsedStartedAt: runningActivity?.started_at,
    actionMenuPinned,
    safeAreaBottom: insets.bottom,
    placeholder,
    keyboardVerticalOffset,
    composerLayout,
    modelControl: composerModelControl,
  });
  const pendingChoice = useMemo(() => latestPendingChoice(events), [events]);
  // A listed Session proves the prompt is on screen with status "blocked". The
  // hidden Brain host carries no live status, so a pending (not abandoned)
  // choice counts as live there; the daemon re-checks the screen before any key.
  const pendingChoiceLive =
    Boolean(pendingChoice) &&
    connectionState === "connected" &&
    (workerInfo?.status === undefined || (workerSessionListFresh && workerInfo.status === "blocked"));
  const answerPendingChoice = useCallback(
    async (payload: ChoiceAnswerPayload) => {
      if (connectionState !== "connected" || !serverId || !workerId) {
        throw new Error("Daemon is not connected.");
      }
      await wsClient.answerChoice(serverId, workerId, payload);
    },
    [connectionState, serverId, workerId],
  );
  const choicePrompt = useMemo<InterfaceChoicePrompt | null>(
    () =>
      pendingChoice?.call_id
        ? {
            callId: pendingChoice.call_id,
            choice: pendingChoice.choice,
            live: pendingChoiceLive,
            onSubmit: answerPendingChoice,
          }
        : null,
    [answerPendingChoice, pendingChoice, pendingChoiceLive],
  );
  const terminalActionPrompt = useMemo(() => {
    // Live pane fact only: require the connect snapshot (worker_session_list) for this WebSocket
    // connection generation so retained pre-disconnect snapshots cannot flash.
    // A structured transcript choice owns the prompt; never show both.
    if (!workerId || !workerSessionListFresh || pendingChoice) {
      return null;
    }
    return buildTerminalActionPrompt({
      status: workerInfo?.status,
      summary: workerInfo?.summary,
      lastOutputLines: workerInfo?.lastOutputLines,
      provider: workerInfo?.provider,
      approvals: workerInfo?.approvals,
      scopeKey: liveActionPromptScopeKey({
        workerId,
        processId: workerInfo?.processId,
        startedAt: workerInfo?.startedAt,
        connectionGeneration,
      }),
    });
  }, [
    workerId,
    workerInfo?.provider,
    workerInfo?.approvals,
    workerInfo?.lastOutputLines,
    workerInfo?.processId,
    workerInfo?.startedAt,
    workerInfo?.status,
    workerInfo?.summary,
    workerSessionListFresh,
    connectionGeneration,
    pendingChoice,
  ]);
  const answerTerminalAction = useCallback(
    (option: TerminalActionPromptOption) => {
      if (connectionState !== "connected" || !serverId || !workerId) {
        throw new Error("Daemon is not connected.");
      }
      if (option.approvalOptionId && terminalActionPrompt?.approvalId) {
        return wsClient.decideApproval(
          serverId,
          workerId,
          terminalActionPrompt.approvalId,
          option.approvalOptionId,
        );
      }
      return wsClient.sendKey(serverId, workerId, option.key);
    },
    [workerId, connectionState, serverId, terminalActionPrompt?.approvalId],
  );
  const skillsSheet = useMemo(
    () =>
      React.createElement(CodexSkillsSheet, {
        visible: skillsSheetVisible,
        serverId,
        cwd: workerInfo?.cwd,
        chrome,
        theme,
        onSelectSkill: (skill) => {
          closeSkillsSheet();
          controller.insertSkillMention(skill);
        },
        onClose: closeSkillsSheet,
      }),
    [
      workerInfo?.cwd,
      chrome,
      closeSkillsSheet,
      controller.insertSkillMention,
      serverId,
      skillsSheetVisible,
      theme,
    ],
  );
  const statusSheet = useMemo(
    () =>
      React.createElement(CodexStatusSheet, {
        visible: statusSheetVisible,
        state: statusState,
        chrome,
        theme,
        onRetry: loadStatus,
        onSwitchToTerminal: onSwitchToTerminal
          ? switchFromStatusToTerminal
          : undefined,
        onClose: closeStatusSheet,
      }),
    [
      chrome,
      closeStatusSheet,
      onSwitchToTerminal,
      loadStatus,
      statusSheetVisible,
      statusState,
      switchFromStatusToTerminal,
      theme,
    ],
  );
  const sheets = useMemo(
    () => React.createElement(React.Fragment, null, skillsSheet, statusSheet),
    [skillsSheet, statusSheet],
  );
  const bodyProps = useInterfaceChatBodyProps({
    screenFocused,
    serverId,
    serverUrl,
    daemonId,
    workerId,
    workerProcessId: workerInfo?.processId,
    workerStartedAt: workerInfo?.startedAt,
    workerCwd: workerInfo?.cwd,
    connectionState,
    conversation,
    events,
    pendingUserMessages,
    turnFocusAnchorAliases,
    // While Claude waits on a choice the card says so; a Working row would
    // read as stuck.
    runningActivity: pendingChoiceLive ? undefined : runningActivity,
    onBrainWorkEventActivate,
    openSessionIds,
    brainCurrentWork,
    loading,
    error,
    draft,
    attachments,
    composerPresentation,
    topChromeInset,
    terminalActionPrompt,
    choicePrompt,
    timeline,
    jumpLabel,
    emptyTitle,
    emptyBody,
    composerInput,
    controller: editAwareController,
    chrome,
    theme,
    onSwitchToTerminal,
    setDraft: setObservedDraft,
    onToggleActionMenu: toggleActionMenu,
    onDismissActionMenu: dismissActionMenu,
    onModelControlPress: handleModelControlPress,
    onTerminalActionAnswer: answerTerminalAction,
    onKeyboardLifecycleInvalidate: handleKeyboardLifecycleInvalidate,
    showUnavailableAction,
    composerAccessory: renderedComposerAccessory,
    quickReplies,
    composerEditBar: messageActions.accessory,
    skillsSheet: sheets,
  });
  return {
    bodyProps,
    messageActions: {
      environment: messageActions.environment,
      replyIds: messageActions.replyIds,
      onEdit: messageActions.onEdit,
    },
  };
}

function latestChatTimelineTimestamp(
  conversation: CodexConversation | null,
  pendingUserMessages: PendingUserMessage[],
) {
  let latest = 0;
  conversation?.events.forEach((event: CodexConversationEvent) => {
    const timestamp = new Date(event.timestamp || "").getTime();
    if (Number.isFinite(timestamp) && timestamp > latest) {
      latest = timestamp;
    }
  });
  pendingUserMessages.forEach((message) => {
    const timestamp = new Date(message.createdAt).getTime();
    if (Number.isFinite(timestamp) && timestamp > latest) {
      latest = timestamp;
    }
  });
  return latest > 0 ? new Date(latest).toISOString() : undefined;
}
