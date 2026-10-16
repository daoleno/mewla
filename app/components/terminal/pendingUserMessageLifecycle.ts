export const PENDING_MESSAGE_RETRY_ACCESSIBILITY_LABEL =
  "Retry sending message";
export const PENDING_MESSAGE_SEND_AGAIN_ACCESSIBILITY_LABEL =
  "Send message again";
export const PENDING_MESSAGE_DISCARD_ACCESSIBILITY_LABEL =
  "Discard unsent message";

/**
 * How long a send may wait while the connection is silent. A socket that died
 * without a close event never answers, and a row must not wait on it
 * silently. A live connection is never silent this long (the daemon sends a
 * frame at least every 10 s), and an acknowledgement on a live connection can
 * legitimately take minutes while the provider is busy, so silence, not the
 * acknowledgement's age, is what times a send out.
 */
export const PENDING_SEND_SILENCE_DEADLINE_MS = 30_000;

/**
 * Milliseconds until a send counts as unacknowledged on a silent connection,
 * or 0 once it does. Any frame after the send restarts the wait.
 */
export function pendingSendSilenceRemainingMs(args: {
  sentAt: number;
  lastFrameAt?: number;
  now: number;
}) {
  const quietSince = Math.max(args.sentAt, args.lastFrameAt ?? args.sentAt);
  return Math.max(0, quietSince + PENDING_SEND_SILENCE_DEADLINE_MS - args.now);
}

/**
 * pending: in flight. failed: not delivered (rejected, or the connection or
 * acknowledgement was lost); Retry reuses the receipt, so it never sends
 * twice. uncertain: the computer could not prove delivery and will not replay
 * the receipt; only a fresh send or Discard settles it.
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
  /** True after one transparent stale-receipt auto-retry with a fresh identity. */
  staleReceiptAutoRetried?: boolean;
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
    staleReceiptAutoRetried?: boolean;
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
    staleReceiptAutoRetried:
      attempt.staleReceiptAutoRetried ?? message.staleReceiptAutoRetried,
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

/** What the user can do with a row the computer did not take. */
export type PendingUserMessageAction = "retry" | "discard";

export type PendingDeliveryOutcome =
  | { kind: "sent" }
  | { kind: "pending"; code?: string; message?: string }
  | {
      kind: "failed";
      failure: { requestId: string; code: string; message: string };
    }
  | { kind: "connection_closed" }
  | { kind: "ack_timeout" };

export type PendingDeliverySettlement =
  | { kind: "acknowledge" }
  | { kind: "reject"; code: string; message: string }
  | { kind: "uncertain"; code: string; message: string };

export const CONNECTION_CLOSED_SEND_ERROR =
  "Not delivered: the connection closed before your computer confirmed it.";
export const ACK_TIMEOUT_SEND_ERROR =
  "Not delivered: your computer stopped answering before it confirmed it.";
export const UNCERTAIN_SEND_ERROR =
  "Your computer couldn't confirm the agent received this. Send it again only if the agent didn't get it.";

/**
 * Every send outcome the computer did not acknowledge settles the row into a
 * visible state with its own actions. Nothing is left pending on a send the
 * computer may never have seen.
 */
export function settlePendingDelivery(
  outcome: PendingDeliveryOutcome,
): PendingDeliverySettlement {
  switch (outcome.kind) {
    case "sent":
      return { kind: "acknowledge" };
    case "pending":
      // Without a code the computer holds the input (accepted by the
      // provider, or the same receipt is already in flight): its receipt or
      // provider echo settles the row. Only input_uncertain is unproven.
      return outcome.code === "input_uncertain"
        ? {
            kind: "uncertain",
            code: outcome.code,
            message: UNCERTAIN_SEND_ERROR,
          }
        : { kind: "acknowledge" };
    case "failed":
      return {
        kind: "reject",
        code: outcome.failure.code,
        message: outcome.failure.message,
      };
    case "connection_closed":
      return {
        kind: "reject",
        code: "connection_closed",
        message: CONNECTION_CLOSED_SEND_ERROR,
      };
    case "ack_timeout":
      return {
        kind: "reject",
        code: "ack_timeout",
        message: ACK_TIMEOUT_SEND_ERROR,
      };
  }
}

export type PendingLifecyclePresentation = {
  lifecycle: PendingUserMessageLifecycle;
  label: string;
  accessibilityLabel: string;
  /** The resend action's label once settled. */
  retryLabel?: "Retry" | "Send again";
};

export function presentPendingUserMessageLifecycle(
  message: Pick<PendingUserMessageLifecycleFields, "lifecycle">,
): PendingLifecyclePresentation {
  if (message.lifecycle === "failed") {
    return {
      lifecycle: "failed",
      label: "Not delivered",
      accessibilityLabel: "Message not delivered",
      retryLabel: "Retry",
    };
  }
  if (message.lifecycle === "uncertain") {
    return {
      lifecycle: "uncertain",
      label: "Delivery unknown",
      accessibilityLabel: "Message delivery unknown",
      retryLabel: "Send again",
    };
  }
  return {
    lifecycle: "pending",
    // Pending is compact status-mark/a11y only (outboundSentClock), never a
    // bubble outline; bubble keeps busy without a status-only a11y label.
    label: "",
    accessibilityLabel: "Message pending provider transcript",
  };
}

/** True while transport is in-flight; false once failed or acknowledged. */
export function showsPendingSendStatusMark(args: {
  pending?: boolean;
  lifecycle?: PendingUserMessageLifecycle;
}): boolean {
  return Boolean(args.pending) && (args.lifecycle ?? "pending") === "pending";
}

/**
 * The stale-receipt auto-retry bound. A `stale_receipt_invalidated` failure
 * means the daemon found a legacy receipt binding for a different payload and
 * proved no provider mutation: the app invalidates that identity and
 * transparently resubmits the same logical input once with a fresh identity.
 * Any later stale failure for the same row is surfaced to the user instead.
 */
export function staleReceiptAutoRetryPolicy(
  message: Pick<
    PendingUserMessageLifecycleFields,
    "staleReceiptAutoRetried"
  >,
  failure: { code?: string },
):
  | { autoRetry: false; reason: "not_stale" | "already_retried" }
  | { autoRetry: true } {
  if (failure.code !== "stale_receipt_invalidated") {
    return { autoRetry: false, reason: "not_stale" };
  }
  if (message.staleReceiptAutoRetried) {
    return { autoRetry: false, reason: "already_retried" };
  }
  return { autoRetry: true };
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
