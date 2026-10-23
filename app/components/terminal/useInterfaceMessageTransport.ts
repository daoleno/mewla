import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type SetStateAction,
} from "react";
import { Keyboard } from "react-native";
import type { ConnectionState } from "../../store/workers";
import type { ProviderActivity } from "../../services/codexConversation";
import type { CodexSlashCommand } from "../../services/websocket";
import { wsClient } from "../../services/websocket";
import type {
  ComposerAttachment,
  PendingUserMessage,
  PendingUserMessageAttempt,
  PendingUserMessageInput,
  PendingUserMessageSettlement,
} from "./InterfaceChatSession";
import { messageOutbox } from "../../services/messageOutboxRuntime";
import type {
  OutboxEntry,
  OutboxEvent,
  SendAttachment,
} from "../../services/messageOutbox";
import {
  UNCERTAIN_SEND_ERROR,
  UNDELIVERED_SEND_ERROR,
  type PendingUserMessageAction,
} from "./pendingUserMessageLifecycle";
import { submitProviderCommandAsUserInput } from "./providerCommandSubmission";
import {
  beginComposerStop,
  reconcileComposerStopLatch,
  releaseComposerStopLatch,
} from "./composerStopLatch";

interface UseInterfaceMessageTransportInput {
  serverId: string;
  workerId: string;
  conversationScopeKey?: string;
  connectionState: ConnectionState;
  runningActivity?: ProviderActivity;
  draft: string;
  attachments: ComposerAttachment[];
  pendingUserMessages: PendingUserMessage[];
  setDraft(value: string): void;
  restoreDraft(value: string): void;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  clearComposerNativeText(): void;
  addPendingUserMessage(message: PendingUserMessageInput): string;
  beginPendingUserMessageAttempt(
    id: string,
    attempt: PendingUserMessageAttempt,
  ): void;
  settlePendingUserMessage(
    id: string,
    settlement: PendingUserMessageSettlement,
  ): void;
  discardPendingUserMessage(id: string): void;
  requestTurnFocus(pendingMessageId: string): void;
}

/** An acknowledged send whose receipt row has not arrived this long is logged. */
const PENDING_UNRECONCILED_REPORT_MS = 45_000;

export function useInterfaceMessageTransport({
  serverId,
  workerId,
  conversationScopeKey,
  connectionState,
  runningActivity,
  draft,
  attachments,
  pendingUserMessages,
  setDraft,
  restoreDraft,
  setAttachments,
  clearComposerNativeText,
  addPendingUserMessage,
  beginPendingUserMessageAttempt,
  settlePendingUserMessage,
  discardPendingUserMessage,
  requestTurnFocus,
}: UseInterfaceMessageTransportInput) {
  const [sending, setSending] = useState(false);
  const [interrupting, setInterrupting] = useState(false);
  const [operationalError, setOperationalError] = useState<string>();
  const sendLockedRef = useRef(false);
  const interruptActivityIdRef = useRef<string | undefined>(undefined);
  const currentDraftRef = useRef(draft);
  const currentAttachmentsRef = useRef(attachments);
  const pendingUserMessagesRef = useRef(pendingUserMessages);
  currentDraftRef.current = draft;
  currentAttachmentsRef.current = attachments;
  pendingUserMessagesRef.current = pendingUserMessages;

  const unlockSend = useCallback(() => {
    sendLockedRef.current = false;
    setSending(false);
  }, []);

  useEffect(
    () => () => {
      sendLockedRef.current = false;
      interruptActivityIdRef.current = undefined;
    },
    [],
  );

  useEffect(() => {
    const reconciled = reconcileComposerStopLatch(
      interruptActivityIdRef.current,
      runningActivity?.id,
    );
    if (reconciled === interruptActivityIdRef.current) {
      return;
    }
    interruptActivityIdRef.current = reconciled;
    setInterrupting(false);
  }, [runningActivity?.id]);

  useEffect(() => {
    setOperationalError(undefined);
  }, [workerId, conversationScopeKey, serverId]);

  // Which row shows which queued message. A row is made once per outbox id.
  const rowIdByRequestRef = useRef(new Map<string, string>());
  const rowIdFor = useCallback((requestId: string) => {
    const known = rowIdByRequestRef.current.get(requestId);
    if (known) return known;
    return pendingUserMessagesRef.current.find(
      (message) => message.dispatchRequestId === requestId,
    )?.id;
  }, []);

  const showQueuedEntry = useCallback(
    (entry: OutboxEntry) => {
      if (rowIdFor(entry.id)) {
        return;
      }
      const rowId = addPendingUserMessage({
        body: entry.text.trim(),
        sentText: entry.text.trim(),
        attachments: (entry.attachments ?? []).map((file) => ({
          name: file.name,
          path: file.path,
          mimeType: file.content_type,
        })),
        lifecycle: entry.state === "undeliverable" ? "failed" : "pending",
        dispatchRequestId: entry.id,
        ...(entry.state === "undeliverable"
          ? { failureCode: entry.failureCode, failureMessage: UNDELIVERED_SEND_ERROR }
          : {}),
      });
      rowIdByRequestRef.current.set(entry.id, rowId);
    },
    [addPendingUserMessage, rowIdFor],
  );

  // The outbox owns delivery; this only keeps the rows honest. Messages that
  // were queued before this screen opened (or before an app restart) get their
  // row back, and every answer settles the row it belongs to.
  useEffect(() => {
    if (!serverId || !workerId) {
      return;
    }
    const mine = (entry: OutboxEntry) =>
      entry.serverId === serverId &&
      entry.workerId === workerId &&
      (entry.conversationScopeKey ?? "") === (conversationScopeKey ?? "");
    const showWaiting = () => {
      for (const entry of messageOutbox.entriesFor(
        serverId,
        workerId,
        conversationScopeKey,
      )) {
        showQueuedEntry(entry);
      }
    };
    showWaiting();
    void messageOutbox.hydrate().then(showWaiting);
    const unreconciledTimers = new Set<ReturnType<typeof setTimeout>>();
    const unsubscribe = messageOutbox.subscribe((event: OutboxEvent) => {
      if (event.kind === "changed") {
        if (event.serverId === serverId) showWaiting();
        return;
      }
      if (!mine(event.entry)) {
        return;
      }
      const entry = event.entry;
      if (event.kind === "reidentified") {
        const rowId = rowIdFor(event.previousId);
        if (rowId) {
          rowIdByRequestRef.current.delete(event.previousId);
          rowIdByRequestRef.current.set(entry.id, rowId);
          beginPendingUserMessageAttempt(rowId, { requestId: entry.id });
        }
        return;
      }
      const rowId = rowIdFor(entry.id);
      if (!rowId) {
        return;
      }
      if (event.kind === "accepted") {
        settlePendingUserMessage(rowId, {
          kind: "acknowledge",
          requestId: entry.id,
        });
        // The computer has it, so the row waits for its provider row. Say so
        // if that never comes: the row would sit with its clock for good.
        const waiting = setTimeout(() => {
          unreconciledTimers.delete(waiting);
          const still = pendingUserMessagesRef.current.find(
            (candidate) =>
              candidate.id === rowId && candidate.lifecycle === "pending",
          );
          if (still) {
            wsClient.reportDiagnostic(
              serverId,
              "pending_unreconciled",
              `request=${entry.id} waited=${PENDING_UNRECONCILED_REPORT_MS}ms ` +
                `lastFrameAgeMs=${Math.max(0, Date.now() - (wsClient.lastFrameAt(serverId) ?? 0))} ` +
                `priorEvents=${still.createdAfterEventIds?.length ?? 0}`,
              entry.id,
            );
          }
        }, PENDING_UNRECONCILED_REPORT_MS);
        unreconciledTimers.add(waiting);
      } else if (event.kind === "uncertain") {
        settlePendingUserMessage(rowId, {
          kind: "uncertain",
          requestId: entry.id,
          code: "input_uncertain",
          message: UNCERTAIN_SEND_ERROR,
        });
      } else if (event.kind === "undeliverable") {
        settlePendingUserMessage(rowId, {
          kind: "reject",
          requestId: entry.id,
          code: entry.failureCode ?? "undeliverable",
          message: UNDELIVERED_SEND_ERROR,
        });
        wsClient.reportDiagnostic(
          serverId,
          "send_undeliverable",
          `request=${entry.id} code=${entry.failureCode ?? ""} attempts=${entry.attempts}`,
          entry.id,
        );
      }
    });
    return () => {
      unsubscribe();
      for (const timer of unreconciledTimers) clearTimeout(timer);
    };
  }, [
    beginPendingUserMessageAttempt,
    conversationScopeKey,
    rowIdFor,
    serverId,
    settlePendingUserMessage,
    showQueuedEntry,
    workerId,
  ]);

  const submitTextToInterface = useCallback(
    (
      text: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      if (sendLockedRef.current || !serverId || !workerId) {
        return;
      }
      sendLockedRef.current = true;
      setSending(true);
      setOperationalError(undefined);
      // The message is the queue's from this moment: the row is shown, the
      // composer is free, and delivery (now or after a reconnect) is not the
      // person's job.
      const requestId = messageOutbox.mintId();
      const pendingMessageId = addPendingUserMessage({
        body: previousDraft.trim(),
        sentText: text,
        attachments: previousAttachments.map((attachment) => ({
          name: attachment.name,
          path: attachment.path,
          localUri: attachment.localUri,
          mimeType: attachment.mimeType,
        })),
        lifecycle: "pending",
        dispatchRequestId: requestId,
      });
      rowIdByRequestRef.current.set(requestId, pendingMessageId);
      messageOutbox.enqueue({
        id: requestId,
        serverId,
        workerId,
        conversationScopeKey,
        text,
        attachments: toSendAttachments(previousAttachments),
      });
      requestTurnFocus(pendingMessageId);
      setDraft("");
      clearComposerNativeText();
      setAttachments([]);
      currentDraftRef.current = "";
      currentAttachmentsRef.current = [];
      unlockSend();
    },
    [
      workerId,
      addPendingUserMessage,
      clearComposerNativeText,
      conversationScopeKey,
      requestTurnFocus,
      serverId,
      setAttachments,
      setDraft,
      unlockSend,
    ],
  );

  // The only things a person does with a queued message: tap its "!" to send
  // it again, or press and hold it to delete it.
  const actOnPendingUserMessage = useCallback(
    (pendingMessageId: string, action: PendingUserMessageAction) => {
      const message = pendingUserMessages.find(
        (candidate) =>
          candidate.id === pendingMessageId && candidate.lifecycle === "failed",
      );
      if (!message) {
        return;
      }
      if (action === "discard") {
        messageOutbox.discard(message.dispatchRequestId);
        rowIdByRequestRef.current.delete(message.dispatchRequestId);
        discardPendingUserMessage(message.id);
        return;
      }
      if (messageOutbox.resend(message.dispatchRequestId)) {
        return;
      }
      // The queue no longer holds it: queue the row's own text afresh.
      const requestId = messageOutbox.mintId();
      rowIdByRequestRef.current.set(requestId, message.id);
      beginPendingUserMessageAttempt(message.id, { requestId });
      messageOutbox.enqueue({
        id: requestId,
        serverId,
        workerId,
        conversationScopeKey,
        text: message.sentText,
        attachments: toSendAttachments(message.attachments),
      });
    },
    [
      beginPendingUserMessageAttempt,
      conversationScopeKey,
      discardPendingUserMessage,
      pendingUserMessages,
      serverId,
      workerId,
    ],
  );

  // A provider command is only its text: the files in the composer are not part
  // of it, so the row and the daemon request carry none.
  const submitProviderCommandText = useCallback(
    (text: string, previousDraft: string) =>
      submitTextToInterface(text, previousDraft, []),
    [submitTextToInterface],
  );

  const startNewInterfaceChat = useCallback(
    (
      commandText: string = "/new",
      previousDraft: string = commandText,
      previousAttachments: ComposerAttachment[] = [],
    ) => {
      const submittedText = commandText.trim() || "/new";
      if (!runningActivity) {
        Keyboard.dismiss();
      }
      submitProviderCommandAsUserInput(
        submittedText,
        previousDraft,
        previousAttachments,
        submitProviderCommandText,
      );
    },
    [submitProviderCommandText, runningActivity],
  );

  const sendSlashCommandToInterface = useCallback(
    (
      text: string,
      _command?: CodexSlashCommand,
      previousDraft?: string,
      previousAttachments?: ComposerAttachment[],
    ) => {
      submitProviderCommandAsUserInput(
        text,
        previousDraft,
        previousAttachments,
        submitProviderCommandText,
      );
    },
    [submitProviderCommandText],
  );

  const interruptInterface = useCallback(() => {
    if (connectionState !== "connected") {
      return;
    }
    const latch = beginComposerStop(
      interruptActivityIdRef.current,
      runningActivity?.id,
    );
    if (!latch.accepted) {
      return;
    }
    interruptActivityIdRef.current = latch.latchedActivityId;
    const claimedActivityId = latch.latchedActivityId;
    setInterrupting(true);
    setOperationalError(undefined);
    const releaseFailedStop = (message: string) => {
      const previousLatch = interruptActivityIdRef.current;
      const nextLatch = releaseComposerStopLatch(
        previousLatch,
        claimedActivityId,
      );
      interruptActivityIdRef.current = nextLatch;
      if (nextLatch !== previousLatch) {
        setInterrupting(false);
        setOperationalError(message);
      }
    };
    try {
      const receipt = wsClient.sendAction(serverId, workerId, "pause");
      void receipt.outcome.then((outcome) => {
        if (outcome.kind === "failed") {
          releaseFailedStop(outcome.failure.message);
        } else if (outcome.kind === "connection_closed") {
          releaseFailedStop(
            "Stop delivery is uncertain. Check provider state before retrying.",
          );
        }
      });
    } catch (error: any) {
      releaseFailedStop(
        error?.message || "Stop was not dispatched. Try again.",
      );
    }
  }, [workerId, connectionState, serverId, runningActivity?.id]);

  return {
    sending,
    interrupting,
    operationalError,
    submitTextToInterface,
    startNewInterfaceChat,
    sendSlashCommandToInterface,
    interruptInterface,
    actOnPendingUserMessage,
  };
}

function toSendAttachments(
  attachments: ReadonlyArray<{ name: string; path: string; mimeType?: string }>,
): SendAttachment[] {
  return attachments.map((file) => ({
    name: file.name,
    path: file.path,
    ...(file.mimeType ? { content_type: file.mimeType } : {}),
  }));
}
