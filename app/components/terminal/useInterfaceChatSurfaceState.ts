import React, {
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
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
  CHOICE_GONE_NOTICE,
  PROMPT_GONE_NOTICE,
  awaitingChoiceAnswer,
  choiceFromPrompt,
  isChoiceGoneError,
  isPromptGoneError,
  latestPendingChoice,
  visiblePendingChoice,
  type ChoiceAnswer,
  type InterfaceChoicePrompt,
} from "./claudeChoiceModel";
import { liveActionPromptScopeKey } from "../../services/workerSessionListTransport";
import { useCodexSlashCommands } from "./CodexSlashCommands";
import { useInterfaceChatBodyProps } from "./useInterfaceChatBodyProps";
import { finalReplyIdsOf } from "./InterfaceTimelineModel";
import { isEditablePrompt } from "./messageActionsModel";
import type { ComposerKeyActions } from "./composerKeyActions";
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
  }) => ReactNode;
  composerPickerOpen?: (draft: string) => boolean;
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
  composerPickerOpen: pickerOpenFor,
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
  const [promptNotice, setPromptNotice] = useState<string>();
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
  const renderedComposerAccessory = renderComposerAccessory
    ? renderComposerAccessory({
        draft,
        setDraft: setObservedDraft,
      })
    : composerAccessory;
  const composerInput = useInterfaceComposerInput({
    enabled: active && connectionState === "connected",
  });
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
  // ↑ in an empty box edits the last message the person sent, where the app
  // lets it be edited (idle, connected, text only).
  const { environment: messageEnvironment, onEdit: editMessage, cancelEditIfActive } = messageActions;
  const composerKeyActions = useMemo<ComposerKeyActions>(
    () => ({
      editLast: () => {
        for (let index = events.length - 1; index >= 0; index -= 1) {
          const event = events[index];
          if (event.kind !== "user_message") continue;
          const subject = {
            id: event.id,
            role: "user" as const,
            body: event.body ?? "",
            hasAttachments: (event.attachments?.length ?? 0) > 0,
            pending: false,
            streaming: false,
            timestamp: event.timestamp,
          };
          if (!isEditablePrompt(subject, messageEnvironment)) return false;
          editMessage(subject);
          return true;
        }
        return false;
      },
      cancelEdit: cancelEditIfActive,
    }),
    [cancelEditIfActive, editMessage, events, messageEnvironment],
  );
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
  // Prompts the daemon said already passed (a prompt id, or a tool call id for
  // the Brain host); the Session list can lag the screen.
  const [gonePromptIds, setGonePromptIds] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  const bodyController = useMemo(
    () => ({
      ...editAwareController,
      operationalError: editAwareController.operationalError ?? promptNotice,
    }),
    [promptNotice, editAwareController],
  );
  // The hidden Brain host is not on the Session list, so it has no live status
  // and no terminal prompt; its question is read from the transcript instead.
  const hostWithoutLivePrompt = workerInfo?.status === undefined;
  // Live pane fact only: require the connect snapshot (worker_session_list) for this WebSocket
  // connection generation so retained pre-disconnect snapshots cannot flash.
  const livePrompt =
    workerId && workerSessionListFresh && workerInfo?.prompt && !gonePromptIds.has(workerInfo.prompt.id)
      ? workerInfo.prompt
      : undefined;
  const transcriptChoice = useMemo(() => latestPendingChoice(events), [events]);
  const pendingChoice = useMemo(
    () => visiblePendingChoice(events, gonePromptIds),
    [events, gonePromptIds],
  );
  const openPromptId = workerInfo?.prompt?.id;
  const openCallId = pendingChoice?.call_id;
  useEffect(() => {
    setGonePromptIds((existing) => (existing.size ? new Set() : existing));
    setPromptNotice(undefined);
  }, [serverId, workerId]);
  useEffect(() => {
    setPromptNotice(undefined);
  }, [openPromptId, openCallId]);
  const answerPrompt = useCallback(
    async (
      promptId: string,
      answer: { number?: number; answers?: ChoiceAnswer[] },
    ) => {
      if (connectionState !== "connected" || !serverId || !workerId) {
        throw new Error("Daemon is not connected.");
      }
      try {
        await wsClient.answerPrompt(serverId, workerId, { prompt_id: promptId, ...answer });
      } catch (reason) {
        if (isPromptGoneError(reason)) {
          setGonePromptIds((existing) => new Set(existing).add(promptId));
          setPromptNotice(PROMPT_GONE_NOTICE);
          return;
        }
        throw reason;
      }
    },
    [connectionState, serverId, workerId],
  );
  const answerTranscriptChoice = useCallback(
    async (callId: string, answers: ChoiceAnswer[]) => {
      if (connectionState !== "connected" || !serverId || !workerId) {
        throw new Error("Daemon is not connected.");
      }
      try {
        await wsClient.answerChoice(serverId, workerId, { call_id: callId, answers });
      } catch (reason) {
        if (isChoiceGoneError(reason)) {
          setGonePromptIds((existing) => new Set(existing).add(callId));
          setPromptNotice(CHOICE_GONE_NOTICE);
          return;
        }
        throw reason;
      }
    },
    [connectionState, serverId, workerId],
  );
  const choicePrompt = useMemo<InterfaceChoicePrompt | null>(() => {
    const connected = connectionState === "connected";
    if (hostWithoutLivePrompt) {
      const callId = pendingChoice?.call_id;
      return callId && pendingChoice
        ? {
            key: callId,
            choice: pendingChoice.choice,
            connected,
            onSubmit: (answers) => answerTranscriptChoice(callId, answers),
          }
        : null;
    }
    const promptId = livePrompt?.id;
    const choice = choiceFromPrompt(
      livePrompt,
      transcriptChoice?.call_id
        ? { callId: transcriptChoice.call_id, choice: transcriptChoice.choice }
        : null,
    );
    return promptId && choice
      ? {
          key: livePrompt?.call_id ?? promptId,
          choice,
          connected,
          onSubmit: (answers) => answerPrompt(promptId, { answers }),
        }
      : null;
  }, [
    answerPrompt,
    answerTranscriptChoice,
    connectionState,
    hostWithoutLivePrompt,
    livePrompt,
    pendingChoice,
    transcriptChoice,
  ]);
  const terminalActionPrompt = useMemo(() => {
    if (!workerId || !workerSessionListFresh) {
      return null;
    }
    return buildTerminalActionPrompt({
      prompt: livePrompt,
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
    livePrompt,
    workerInfo?.approvals,
    workerInfo?.processId,
    workerInfo?.startedAt,
    workerSessionListFresh,
    connectionGeneration,
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
      if (option.number !== undefined && terminalActionPrompt?.promptId) {
        return answerPrompt(terminalActionPrompt.promptId, { number: option.number });
      }
      throw new Error("This option cannot be answered here.");
    },
    [workerId, connectionState, serverId, terminalActionPrompt?.approvalId, terminalActionPrompt?.promptId, answerPrompt],
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
    // While the agent waits on a choice the card says so; a Working row would
    // read as busy. That holds after the card is hidden as "passed" too, until
    // the transcript settles the question. The composer and controller still
    // see the real activity.
    runningActivity: awaitingChoiceAnswer(events) ? undefined : runningActivity,
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
    controller: bodyController,
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
    composerKeyActions,
    composerPickerOpen: pickerOpenFor?.(draft) ?? false,
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
