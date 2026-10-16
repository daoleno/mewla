import { describe, expect, mock, test } from "bun:test";
import { mergePendingUserMessagesIntoTimeline } from "./InterfaceTimelineModel";
import {
  PENDING_SEND_SILENCE_DEADLINE_MS,
  pendingSendSilenceRemainingMs,
  presentPendingUserMessageLifecycle,
  settlePendingDelivery,
  showsPendingSendStatusMark,
  type PendingDeliveryOutcome,
} from "./pendingUserMessageLifecycle";
import { projectTimeline } from "./projectTimeline";

mock.module("react-native", () => ({ Platform: { OS: "web" } }));
mock.module("../../services/auth", () => ({
  buildAuthorizationHeader: async () => "test-authorization",
}));
mock.module("../../services/connectionIssue", () => ({
  diagnoseConnectionIssue: async () => null,
}));

const { interfaceChatThreadReducer } = await import("./InterfaceChatSession");

type ThreadState = Parameters<typeof interfaceChatThreadReducer>[0];
type ConversationEvents = NonNullable<ThreadState["conversation"]>["events"];

// The Brain thread on 2026-10-16 (ids from the Linux Brain store): Brain asks
// for two Tailscale approvals, the reply to it never reaches the computer, and
// two later sends are admitted with their receipts.
const BRAIN_ASK = {
  id: "07ac686b:assistant:0431",
  seq: 1,
  kind: "assistant_message" as const,
  timestamp: "2026-10-16T04:31:52.095Z",
  body: "Approve the two Tailscale SSH links.",
};
const BRAIN_RESUMED = {
  id: "07ac686b:assistant:0516",
  seq: 2,
  kind: "assistant_message" as const,
  timestamp: "2026-10-16T05:16:00.464Z",
  body: "Still waiting on the Tailscale approvals.",
};
const LOGGED_IN = {
  id: "mvaix1nl_vo5j29",
  seq: 3,
  kind: "user_message" as const,
  timestamp: "2026-10-16T05:24:59.923Z",
  body: "登录好了",
};
const BRAIN_REPLY = {
  id: "07ac686b:assistant:0525",
  seq: 4,
  kind: "assistant_message" as const,
  timestamp: "2026-10-16T05:25:34.581Z",
  body: "The Perpetuo release is running.",
};
const IN_USE = {
  id: "mvake66p_lqay1h",
  seq: 5,
  kind: "user_message" as const,
  timestamp: "2026-10-16T06:06:18.612Z",
  body: "我确实在用",
};
const LOST_BODY = "perpetuo tailscale 我已经批准了";

function emptyState(): ThreadState {
  return {
    cacheKey: "server-a:agent:%353",
    conversation: null,
    loading: false,
    error: null,
    pendingUserMessages: [],
    turnFocusAnchorAliases: new Map(),
    streamCursor: { revision: 0, generation: 1 },
    awaitingSnapshot: false,
    resyncToken: 0,
  };
}

function snapshot(current: ThreadState, events: ConversationEvents) {
  return interfaceChatThreadReducer(current, {
    type: "snapshot",
    generation: 1,
    payload: {
      request_id: "stream-1",
      conversation_id: "brain-thread",
      revision: current.streamCursor.revision + 1,
      conversation: { available: true, session_id: "brain-thread", events },
    },
  });
}

function send(
  current: ThreadState,
  id: string,
  requestId: string,
  body: string,
  createdAt: string,
) {
  return interfaceChatThreadReducer(current, {
    type: "add_pending_user_message",
    message: {
      id,
      body,
      sentText: body,
      attachments: [],
      createdAt,
      lifecycle: "pending",
      dispatchRequestId: requestId,
    },
  });
}

function settle(
  current: ThreadState,
  id: string,
  requestId: string,
  outcome: PendingDeliveryOutcome,
) {
  return interfaceChatThreadReducer(current, {
    type: "settle_pending_user_message",
    id,
    settlement: { ...settlePendingDelivery(outcome), requestId },
  });
}

function rendered(
  current: ThreadState,
  onAction?: (id: string, action: "retry" | "discard") => void,
) {
  return mergePendingUserMessagesIntoTimeline(
    projectTimeline(current.conversation?.events ?? [], null).items,
    current.pendingUserMessages,
    onAction,
  );
}

function renderedMessages(current: ThreadState) {
  return rendered(current).flatMap((item) =>
    item.type === "message" ? [`${item.role}:${item.body}`] : [],
  );
}

/** Replays the observed thread up to the send of 「我确实在用」. */
function observedThread(settleLost?: PendingDeliveryOutcome) {
  let current = snapshot(emptyState(), [BRAIN_ASK]);
  current = send(
    current,
    "local-lost",
    "receipt-never-admitted",
    LOST_BODY,
    "2026-10-16T04:53:00.000Z",
  );
  if (settleLost) {
    current = settle(current, "local-lost", "receipt-never-admitted", settleLost);
  }
  current = snapshot(current, [BRAIN_ASK, BRAIN_RESUMED]);
  current = send(
    current,
    "local-logged-in",
    LOGGED_IN.id,
    LOGGED_IN.body,
    "2026-10-16T05:24:59.355Z",
  );
  current = snapshot(current, [BRAIN_ASK, BRAIN_RESUMED, LOGGED_IN, BRAIN_REPLY]);
  current = send(
    current,
    "local-in-use",
    IN_USE.id,
    IN_USE.body,
    "2026-10-16T06:06:18.038Z",
  );
  return snapshot(current, [
    BRAIN_ASK,
    BRAIN_RESUMED,
    LOGGED_IN,
    BRAIN_REPLY,
    IN_USE,
  ]);
}

describe("a send the computer never admitted", () => {
  test("stays where it was sent and is never carried under newer messages", () => {
    // No settlement: even a row still waiting must not follow later receipts.
    const current = observedThread();
    expect(current.pendingUserMessages.map((row) => row.id)).toEqual([
      "local-lost",
    ]);
    expect(renderedMessages(current)).toEqual([
      `assistant:${BRAIN_ASK.body}`,
      `user:${LOST_BODY}`,
      `assistant:${BRAIN_RESUMED.body}`,
      `user:${LOGGED_IN.body}`,
      `assistant:${BRAIN_REPLY.body}`,
      `user:${IN_USE.body}`,
    ]);
  });

  test("a closed connection says Not delivered with Retry and Discard at once", () => {
    const actions: string[] = [];
    let current = snapshot(emptyState(), [BRAIN_ASK]);
    current = send(
      current,
      "local-lost",
      "receipt-never-admitted",
      LOST_BODY,
      "2026-10-16T04:53:00.000Z",
    );
    current = settle(current, "local-lost", "receipt-never-admitted", {
      kind: "connection_closed",
    });

    const row = rendered(current, (id, action) => actions.push(`${action}:${id}`))
      .find((item) => item.id === "local-lost");
    expect(row).toMatchObject({
      type: "message",
      role: "user",
      body: LOST_BODY,
      pending: true,
      pendingLifecycle: "failed",
      pendingLifecycleLabel: "Not delivered",
      pendingRetryLabel: "Retry",
    });
    expect(
      showsPendingSendStatusMark({
        pending: true,
        lifecycle: current.pendingUserMessages[0]?.lifecycle,
      }),
    ).toBe(false);
    if (row?.type !== "message") throw new Error("expected a message row");
    row.onRetryPending?.();
    row.onDiscardPending?.();
    expect(actions).toEqual(["retry:local-lost", "discard:local-lost"]);
  });

  test("the observed thread renders each message once, in its own place", () => {
    const current = observedThread({ kind: "connection_closed" });
    expect(current.pendingUserMessages).toEqual([
      expect.objectContaining({ id: "local-lost", lifecycle: "failed" }),
    ]);
    const messages = renderedMessages(current);
    expect(messages).toEqual([
      `assistant:${BRAIN_ASK.body}`,
      `user:${LOST_BODY}`,
      `assistant:${BRAIN_RESUMED.body}`,
      `user:${LOGGED_IN.body}`,
      `assistant:${BRAIN_REPLY.body}`,
      `user:${IN_USE.body}`,
    ]);
    expect(messages.filter((message) => message === `user:${IN_USE.body}`))
      .toHaveLength(1);

    const discarded = interfaceChatThreadReducer(current, {
      type: "discard_pending_user_message",
      id: "local-lost",
    });
    expect(discarded.pendingUserMessages).toEqual([]);
    expect(renderedMessages(discarded)).not.toContain(`user:${LOST_BODY}`);
  });

  test("Retry reuses the receipt, and its admission replaces the row once", () => {
    let current = observedThread({ kind: "connection_closed" });
    current = interfaceChatThreadReducer(current, {
      type: "begin_pending_user_message_attempt",
      id: "local-lost",
      requestId: "receipt-never-admitted",
    });
    expect(current.pendingUserMessages[0]?.lifecycle).toBe("pending");
    const admitted = {
      id: "receipt-never-admitted",
      seq: 6,
      kind: "user_message" as const,
      timestamp: "2026-10-16T06:10:00.000Z",
      body: LOST_BODY,
    };
    current = snapshot(current, [
      BRAIN_ASK,
      BRAIN_RESUMED,
      LOGGED_IN,
      BRAIN_REPLY,
      IN_USE,
      admitted,
    ]);
    expect(current.pendingUserMessages).toEqual([]);
    const messages = renderedMessages(current);
    expect(messages.filter((message) => message === `user:${LOST_BODY}`))
      .toHaveLength(1);
    expect(messages.at(-1)).toBe(`user:${LOST_BODY}`);
  });

  test("an in-flight row cannot be discarded", () => {
    const current = send(
      snapshot(emptyState(), [BRAIN_ASK]),
      "local-a",
      "receipt-a",
      "hello",
      "2026-10-16T05:00:00.000Z",
    );
    expect(
      interfaceChatThreadReducer(current, {
        type: "discard_pending_user_message",
        id: "local-a",
      }),
    ).toBe(current);
  });
});

describe("every send outcome settles visibly", () => {
  test("only an acknowledgement leaves the row waiting for its receipt", () => {
    const outcomes: PendingDeliveryOutcome[] = [
      { kind: "connection_closed" },
      { kind: "ack_timeout" },
      { kind: "pending", code: "input_uncertain" },
      {
        kind: "failed",
        failure: {
          requestId: "r",
          code: "input_not_submitted",
          message: "This request was not submitted.",
        },
      },
    ];
    for (const outcome of outcomes) {
      expect(settlePendingDelivery(outcome).kind).not.toBe("acknowledge");
    }
    // The computer holds the input: the receipt or provider echo settles it.
    expect(settlePendingDelivery({ kind: "sent" })).toEqual({
      kind: "acknowledge",
    });
    expect(settlePendingDelivery({ kind: "pending" })).toEqual({
      kind: "acknowledge",
    });
  });

  test("only a silent connection times a send out, never a slow acknowledgement", () => {
    const sentAt = Date.parse("2026-10-16T05:00:00.000Z");
    // Brain admissions on this computer took up to 236.9 s to be accepted
    // while the provider was busy; the connection stayed live meanwhile.
    const slowAckAt = sentAt + 236_937;
    expect(
      pendingSendSilenceRemainingMs({
        sentAt,
        lastFrameAt: slowAckAt - 10_000,
        now: slowAckAt,
      }),
    ).toBe(PENDING_SEND_SILENCE_DEADLINE_MS - 10_000);
    // No frame since the send: a half-open socket.
    expect(
      pendingSendSilenceRemainingMs({
        sentAt,
        lastFrameAt: sentAt - 5_000,
        now: sentAt + PENDING_SEND_SILENCE_DEADLINE_MS,
      }),
    ).toBe(0);
    expect(
      pendingSendSilenceRemainingMs({ sentAt, now: sentAt + 1_000 }),
    ).toBe(PENDING_SEND_SILENCE_DEADLINE_MS - 1_000);
  });

  test("a missing acknowledgement is Not delivered until a late one arrives", () => {
    let current = send(
      snapshot(emptyState(), [BRAIN_ASK]),
      "local-a",
      "receipt-a",
      "hello",
      "2026-10-16T05:00:00.000Z",
    );
    current = settle(current, "local-a", "receipt-a", { kind: "ack_timeout" });
    expect(current.pendingUserMessages[0]).toMatchObject({
      lifecycle: "failed",
      failureCode: "ack_timeout",
    });
    current = settle(current, "local-a", "receipt-a", { kind: "sent" });
    expect(current.pendingUserMessages[0]).toMatchObject({
      lifecycle: "pending",
      failureCode: undefined,
      failureMessage: undefined,
    });
    // A stale attempt's deadline never touches the current attempt.
    expect(
      settle(current, "local-a", "receipt-old", { kind: "ack_timeout" }),
    ).toBe(current);
  });

  test("an unproven delivery says so, offers Send again, and its echo clears it", () => {
    let current = send(
      snapshot(emptyState(), [BRAIN_ASK]),
      "local-a",
      "receipt-a",
      "hello",
      "2026-10-16T05:00:00.000Z",
    );
    current = settle(current, "local-a", "receipt-a", {
      kind: "pending",
      code: "input_uncertain",
    });
    expect(current.pendingUserMessages[0]?.lifecycle).toBe("uncertain");
    expect(presentPendingUserMessageLifecycle({ lifecycle: "uncertain" }))
      .toMatchObject({ label: "Delivery unknown", retryLabel: "Send again" });
    expect(
      rendered(current, () => {}).find((item) => item.id === "local-a"),
    ).toMatchObject({
      pendingLifecycle: "uncertain",
      pendingRetryLabel: "Send again",
      onDiscardPending: expect.any(Function),
    });

    current = snapshot(current, [
      BRAIN_ASK,
      {
        id: "provider-echo",
        seq: 2,
        kind: "user_message",
        timestamp: "2026-10-16T05:00:01.000Z",
        body: "hello",
      },
    ]);
    expect(current.pendingUserMessages).toEqual([]);
  });
});
