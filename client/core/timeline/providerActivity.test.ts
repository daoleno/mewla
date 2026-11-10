import { describe, expect, test } from "bun:test";
import {
  isProviderActivityRunning,
  normalizeCodexConversation,
  type CodexConversationEvent,
  type ProviderActivity,
} from "../conversation/conversation";
import {
  buildTimeline,
  mergePendingUserMessagesIntoTimeline,
  mergeRunningActivityIntoTimeline,
} from "./model";
import { reconcileConversationSnapshot } from "../conversation/reconciliation";

const START = "2026-07-15T01:00:00.000Z";
const LATER = "2026-07-15T01:00:01.000Z";

function activity(
  status: ProviderActivity["status"] = "running",
  id: string = "activity-a",
  startedAt: string = START,
): ProviderActivity {
  return {
    id,
    status,
    started_at: startedAt,
    ...(status === "completed" ||
    status === "failed" ||
    status === "interrupted" ||
    status === "cancelled"
      ? { settled_at: LATER }
      : {}),
  };
}

function localPending(id: string, createdAt: string = LATER) {
  return {
    id,
    body: id,
    sentText: id,
    attachments: [],
    createdAt,
    lifecycle: "pending" as const,
    dispatchRequestId: `request:${id}`,
    dispatchAttemptOrder: 1,
  };
}

describe("provider-native Activity Working", () => {
  test("uses provider-neutral fallback titles for status events", () => {
    const titles = [
      ["failed", "Agent error"],
      ["running", "Agent status"],
      ["warning", "Agent warning"],
      [undefined, "Agent status"],
    ].map(([status, expected], index) => {
      const [item] = buildTimeline([
        {
          id: `status-${index}`,
          seq: index,
          kind: "status",
          status,
        },
      ]);
      expect(item).toMatchObject({ type: "activity", title: expected });
      return item.type === "activity" ? item.title : undefined;
    });

    expect(titles).toEqual([
      "Agent error",
      "Agent status",
      "Agent warning",
      "Agent status",
    ]);
  });

  test("silent reasoning and tool gaps keep one stable Working footer", () => {
    const events: CodexConversationEvent[] = [
      {
        id: "assistant-partial",
        seq: 1,
        timestamp: LATER,
        kind: "assistant_message",
        role: "assistant",
        body: "Partial answer",
        partial: true,
      },
      {
        id: "tool-running",
        seq: 2,
        timestamp: LATER,
        kind: "tool",
        title: "Searching",
        status: "running",
      },
    ];
    const once = mergeRunningActivityIntoTimeline(
      buildTimeline(events),
      activity(),
    );
    const twice = mergeRunningActivityIntoTimeline(once, activity());
    expect(
      twice.filter(
        (item) => item.type === "activity" && item.title === "Working",
      ),
    ).toHaveLength(1);
    expect(twice.at(-1)?.id).toBe("provider-activity:activity-a");
  });

  test("local inputs retain FIFO order before the Working footer", () => {
    const composed = mergePendingUserMessagesIntoTimeline(
      mergeRunningActivityIntoTimeline([], activity()),
      [localPending("pending-a", START), localPending("pending-b", LATER)],
    );
    expect(composed.map((item) => item.id)).toEqual([
      "pending-a",
      "pending-b",
      "provider-activity:activity-a",
    ]);
  });

  test("same-boundary duplicate text rows keep local creation order", () => {
    const history = buildTimeline([
      {
        id: "history",
        seq: 1,
        timestamp: START,
        kind: "assistant_message",
        role: "assistant",
        body: "Earlier answer",
      },
    ]);
    const repeated = (id: string) => ({
      ...localPending(id),
      body: "identical",
      sentText: "identical",
      createdAfterEventIds: ["history"],
    });
    const composed = mergePendingUserMessagesIntoTimeline(
      mergeRunningActivityIntoTimeline(history, activity()),
      [repeated("pending-a"), repeated("pending-b"), repeated("pending-c")],
    );
    expect(composed.map((item) => item.id)).toEqual([
      "history",
      "pending-a",
      "pending-b",
      "pending-c",
      "provider-activity:activity-a",
    ]);
  });

  test("a local input stays at its causal boundary before later assistant chunks", () => {
    const providerTimeline = buildTimeline([
      {
        id: "history",
        seq: 1,
        timestamp: "2026-07-15T00:59:00.000Z",
        kind: "assistant_message",
        body: "Earlier answer",
      },
      {
        id: "assistant-current",
        seq: 2,
        timestamp: LATER,
        kind: "assistant_message",
        body: "Current partial",
        partial: true,
      },
    ]);
    const composed = mergePendingUserMessagesIntoTimeline(
      mergeRunningActivityIntoTimeline(providerTimeline, activity()),
      [
        {
          ...localPending("pending-a", START),
          createdAfterEventIds: ["history"],
        },
      ],
    );
    expect(composed.map((item) => item.id)).toEqual([
      "history",
      "pending-a",
      "assistant-current",
      "provider-activity:activity-a",
    ]);
  });

  test("partialness remains rendering-only after Activity settlement", () => {
    const partial: CodexConversationEvent = {
      id: "assistant-partial",
      seq: 1,
      timestamp: LATER,
      kind: "assistant_message",
      body: "Partial answer",
      partial: true,
    };
    expect(buildTimeline([partial])[0]).toMatchObject({ streaming: true });
    expect(
      isProviderActivityRunning(
        normalizeCodexConversation({
          available: true,
          activity: activity("completed"),
          events: [partial],
        }).activity,
      ),
    ).toBe(false);
  });
});

describe("authoritative Activity snapshots", () => {
  test("unknown legacy lifecycle input cannot resurrect visible Working", () => {
    expect(
      isProviderActivityRunning(
        normalizeCodexConversation({
          available: true,
          source: "codex_rollout",
          turn: activity("running"),
          events: [],
        }).activity,
      ),
    ).toBe(false);
  });

  test("same-thread snapshot exactly clears Activity and events", () => {
    const previous = normalizeCodexConversation({
      available: true,
      session_id: "thread-a",
      activity: activity(),
      events: [
        { id: "history", seq: 1, kind: "assistant_message", body: "old" },
      ],
    });
    const snapshot = reconcileConversationSnapshot(
      previous,
      normalizeCodexConversation({
        available: true,
        session_id: "thread-a",
        events: [],
      }),
      true,
    );
    expect(snapshot.activity).toBeUndefined();
    expect(snapshot.events).toEqual([]);
  });
});

describe("scope-first hydration identity", () => {
  test("normalization rejects lifecycle records without a valid start", () => {
    expect(
      normalizeCodexConversation({
        available: true,
        activity: { id: "bad", status: "running" },
        events: [],
      }).activity,
    ).toBeUndefined();
    expect(
      normalizeCodexConversation({
        available: true,
        activity: { id: "bad", status: "running", started_at: "not-a-date" },
        events: [],
      }).activity,
    ).toBeUndefined();
  });
});
