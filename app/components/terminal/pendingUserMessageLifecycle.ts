export const PENDING_MESSAGE_UNDELIVERED_ACCESSIBILITY_LABEL =
  "Message not delivered. Tap to send again. Press and hold to delete.";

/**
 * pending: waiting in the send queue, or taken by the computer and waiting for
 * the provider's own row. Both look the same: a small clock. uncertain: the
 * computer could not prove delivery and will not replay it; it also looks like
 * pending and only turns failed if no provider row ever arrives. failed: the
 * message could not be delivered (the computer kept refusing it, or the
 * delivery was never confirmed); the one state the person touches.
 */
export type PendingUserMessageLifecycle = "pending" | "failed" | "uncertain";

export type PendingUserMessageLifecycleFields = {
  lifecycle: PendingUserMessageLifecycle;
  dispatchRequestId: string;
  dispatchAttemptOrder: number;
  failureCode?: string;
  failureMessage?: string;
  createdAfterMaxSeq?: number;
  /**
   * Events before this attempt: those visible when it was sent, plus echoes
   * claimed by earlier sends. The row's causal place in the timeline; a later
   * send never moves it.
   */
  createdAfterEventIds?: string[];
  /** Echoes claimed by later sends: FIFO exclusion only, never a place. */
  claimedElsewhereEventIds?: string[];
};

export function beginPendingUserMessageAttempt<
  T extends PendingUserMessageLifecycleFields,
>(
  message: T,
  attempt: {
    requestId: string;
    dispatchAttemptOrder: number;
    createdAfterMaxSeq?: number;
    createdAfterEventIds?: string[];
  },
): T {
  return {
    ...message,
    lifecycle: "pending",
    dispatchRequestId: attempt.requestId,
    dispatchAttemptOrder: attempt.dispatchAttemptOrder,
    failureCode: undefined,
    failureMessage: undefined,
    createdAfterMaxSeq: attempt.createdAfterMaxSeq,
    createdAfterEventIds: attempt.createdAfterEventIds,
    claimedElsewhereEventIds: undefined,
  };
}

export function rejectPendingUserMessage<
  T extends PendingUserMessageLifecycleFields,
>(
  message: T,
  failure: {
    requestId: string;
    code: string;
    message: string;
  },
): T {
  if (message.dispatchRequestId !== failure.requestId) {
    return message;
  }
  return {
    ...message,
    lifecycle: "failed",
    failureCode: failure.code,
    failureMessage: failure.message,
  };
}

/** The receipt could not be proven delivered and will not be replayed. */
export function markPendingUserMessageUncertain<
  T extends PendingUserMessageLifecycleFields,
>(
  message: T,
  uncertainty: {
    requestId: string;
    code: string;
    message: string;
  },
): T {
  if (message.dispatchRequestId !== uncertainty.requestId) {
    return message;
  }
  return {
    ...message,
    lifecycle: "uncertain",
    failureCode: uncertainty.code,
    failureMessage: uncertainty.message,
  };
}

/**
 * A late acknowledgement after the ack deadline: the computer has the input,
 * so the row waits for its receipt row again.
 */
export function acknowledgePendingUserMessage<
  T extends PendingUserMessageLifecycleFields,
>(message: T, requestId: string): T {
  if (
    message.dispatchRequestId !== requestId ||
    message.lifecycle === "pending"
  ) {
    return message;
  }
  return {
    ...message,
    lifecycle: "pending",
    failureCode: undefined,
    failureMessage: undefined,
  };
}

/** What the person can do with a row that could not be delivered. */
export type PendingUserMessageAction = "retry" | "discard";

export type PendingDeliverySettlement =
  | { kind: "acknowledge" }
  | { kind: "reject"; code: string; message: string }
  | { kind: "uncertain"; code: string; message: string };

export const UNDELIVERED_SEND_ERROR =
  "Your computer could not take this message.";
export const UNCERTAIN_SEND_ERROR =
  "Your computer couldn't confirm the agent received this.";

export type PendingLifecyclePresentation = {
  lifecycle: PendingUserMessageLifecycle;
  accessibilityLabel: string;
};

export function presentPendingUserMessageLifecycle(
  message: Pick<PendingUserMessageLifecycleFields, "lifecycle">,
): PendingLifecyclePresentation {
  if (message.lifecycle === "failed") {
    return {
      lifecycle: "failed",
      accessibilityLabel: PENDING_MESSAGE_UNDELIVERED_ACCESSIBILITY_LABEL,
    };
  }
  // Waiting and unconfirmed look the same: no prose, only the clock mark.
  return {
    lifecycle: message.lifecycle,
    accessibilityLabel: "Message pending provider transcript",
  };
}

/** True while the message waits to be delivered or echoed; false once it failed. */
export function showsPendingSendStatusMark(args: {
  pending?: boolean;
  lifecycle?: PendingUserMessageLifecycle;
}): boolean {
  return Boolean(args.pending) && (args.lifecycle ?? "pending") !== "failed";
}

export type ReconcileUserEvent = {
  id: string;
  seq?: number;
  kind: string;
};

export type ProviderEventTurnFocusAlias = {
  providerEventId: string;
  localPendingId: string;
};

export type PendingUserMessageReconciliation<T> = {
  pendingUserMessages: T[];
  providerEventAliases: ProviderEventTurnFocusAlias[];
};

/**
 * Consumes provider/canonical user events against local pending rows.
 *
 * 1. Exact receipt match first (global): user event id === dispatchRequestId.
 *    This is the Brain admission identity and clears Pending without FIFO or
 *    seq-boundary guessing. Message bodies are never identities.
 * 2. Provider-neutral causal FIFO fallback for ordinary provider echoes whose
 *    ids differ from the request receipt.
 *
 * A row stays after its send boundary. An event claimed by an earlier send
 * joins that boundary (the earlier send's echo precedes this row); an event
 * claimed by a later send is only excluded from this row's FIFO and never
 * moves it, so an unsettled row is not carried under newer messages.
 */
export function reconcilePendingUserMessagesAgainstEvents<
  T extends PendingUserMessageLifecycleFields & { id: string },
>(
  pendingUserMessages: T[],
  events: ReconcileUserEvent[],
): PendingUserMessageReconciliation<T> {
  if (pendingUserMessages.length === 0 || events.length === 0) {
    return { pendingUserMessages, providerEventAliases: [] };
  }
  const userEvents = events.filter(
    (event) => event.kind === "user_message" && Boolean(event.id),
  );
  if (userEvents.length === 0) {
    return { pendingUserMessages, providerEventAliases: [] };
  }

  // Send order: attempt order, then presentation order.
  const sendRank = new Map(
    pendingUserMessages
      .map((message, presentationIndex) => ({ message, presentationIndex }))
      .sort(
        (left, right) =>
          finiteAttemptOrder(left.message.dispatchAttemptOrder) -
            finiteAttemptOrder(right.message.dispatchAttemptOrder) ||
          left.presentationIndex - right.presentationIndex,
      )
      .map(({ message }, rank) => [message.id, rank]),
  );
  // Claimed event id -> send rank of the row that claimed it.
  const consumedEventIds = new Map<string, number>();
  const clearedPendingIds = new Set<string>();
  const providerEventAliases: ProviderEventTurnFocusAlias[] = [];

  // Pass 1: exact receipt identity, independent of attempt order / seq bounds.
  for (const message of pendingUserMessages) {
    const receiptId = message.dispatchRequestId.trim();
    if (!receiptId || clearedPendingIds.has(message.id)) {
      continue;
    }
    const exact = userEvents.find(
      (event) =>
        event.id === receiptId &&
        !consumedEventIds.has(event.id),
    );
    if (!exact) {
      continue;
    }
    consumedEventIds.set(exact.id, sendRank.get(message.id)!);
    clearedPendingIds.add(message.id);
    providerEventAliases.push({
      providerEventId: exact.id,
      localPendingId: message.id,
    });
  }

  const remainingForFifo = pendingUserMessages.filter(
    (message) => !clearedPendingIds.has(message.id),
  );
  if (remainingForFifo.length === 0) {
    return { pendingUserMessages: [], providerEventAliases };
  }

  const attempts = remainingForFifo
    .map((message, presentationIndex) => ({ message, presentationIndex }))
    .sort((left, right) => {
      const byAttempt =
        finiteAttemptOrder(left.message.dispatchAttemptOrder) -
        finiteAttemptOrder(right.message.dispatchAttemptOrder);
      return byAttempt || left.presentationIndex - right.presentationIndex;
    });
  const remainingById = new Map<string, T>();
  for (const { message } of attempts) {
    const priorEventIds = new Set([
      ...(message.createdAfterEventIds ?? []),
      ...(message.claimedElsewhereEventIds ?? []),
    ]);
    const providerEvent = userEvents.find((event) => {
      if (priorEventIds.has(event.id) || consumedEventIds.has(event.id)) {
        return false;
      }
      return !(
        typeof message.createdAfterMaxSeq === "number" &&
        Number.isFinite(message.createdAfterMaxSeq) &&
        typeof event.seq === "number" &&
        event.seq <= message.createdAfterMaxSeq
      );
    });
    if (providerEvent) {
      consumedEventIds.set(providerEvent.id, sendRank.get(message.id)!);
      clearedPendingIds.add(message.id);
      providerEventAliases.push({
        providerEventId: providerEvent.id,
        localPendingId: message.id,
      });
      continue;
    }
    if (consumedEventIds.size === 0) {
      remainingById.set(message.id, message);
      continue;
    }
    const order = sendRank.get(message.id)!;
    const claimedEarlier: string[] = [];
    const claimedLater: string[] = [];
    for (const [eventId, claimantOrder] of consumedEventIds) {
      (claimantOrder < order ? claimedEarlier : claimedLater).push(eventId);
    }
    remainingById.set(message.id, {
      ...message,
      ...(claimedEarlier.length > 0
        ? {
            createdAfterEventIds: Array.from(
              new Set([
                ...(message.createdAfterEventIds ?? []),
                ...claimedEarlier,
              ]),
            ),
          }
        : {}),
      ...(claimedLater.length > 0
        ? {
            claimedElsewhereEventIds: Array.from(
              new Set([
                ...(message.claimedElsewhereEventIds ?? []),
                ...claimedLater,
              ]),
            ),
          }
        : {}),
    });
  }
  return {
    pendingUserMessages: pendingUserMessages.flatMap((message) => {
      if (clearedPendingIds.has(message.id)) {
        return [];
      }
      const remaining = remainingById.get(message.id);
      return remaining ? [remaining] : [];
    }),
    providerEventAliases,
  };
}

function finiteAttemptOrder(value: number) {
  return Number.isFinite(value) ? value : Number.MAX_SAFE_INTEGER;
}
