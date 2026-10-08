import { describe, expect, test } from "bun:test";
import {
  normalizeCodexConversation,
  type CodexConversationEvent,
} from "../../services/codexConversation";
import { buildTimeline } from "./InterfaceTimelineModel";
import { reconcilePendingUserMessagesAgainstEvents } from "./pendingUserMessageLifecycle";
import { timelineItemsSemanticEqual } from "./timelineItemsSemanticEqual";
import {
  formatTaskDuration,
  formatTaskTokens,
  markdownPreview,
  taskNotificationTimelineItem,
} from "./taskNotificationCardModel";

// Wire shape the daemon projects from the real Brain notification
// (task a3e33bf1f19bac0f0, Claude Code 2.1.289).
const agentDone: CodexConversationEvent = {
  id: "07ac686b:63e9f40f:user",
  seq: 747700,
  timestamp: "2026-10-07T01:39:13.353Z",
  kind: "status",
  title: 'Agent "Check product name conflicts" finished',
  body:
    "**Verdict:** I'd treat the name as a weak candidate, mainly because the [.com](https://example.com) is parked.\n\n" +
    "**1. Domain (observed).** It's parked.\n- Registered since 2003.",
  status: "done",
  call_id: "toolu_017pfJ9FvTCbE6B2KQwXtm3h",
  source: "task_notification",
  task_id: "a3e33bf1f19bac0f0",
  task_tokens: 53095,
  task_tool_uses: 10,
  task_duration_ms: 121781,
};

describe("task notification card model", () => {
  test("projects a finished agent as a compact system card", () => {
    const item = taskNotificationTimelineItem(agentDone);
    expect(item).toEqual({
      type: "task-notification",
      id: "07ac686b:63e9f40f:user",
      timestamp: "2026-10-07T01:39:13.353Z",
      kindLabel: "Agent",
      title: "Check product name conflicts",
      detail: undefined,
      status: "done",
      statusLabel: "Done",
      tone: "accent",
      icon: "checkmark-circle",
      facts: ["2m 2s", "10 tool uses", "53.1k tokens"],
      body: agentDone.body!,
      preview:
        "Verdict: I'd treat the name as a weak candidate, mainly because the .com is parked.",
      details: [
        { label: "Task", value: "a3e33bf1f19bac0f0" },
        { label: "Tool call", value: "toolu_017pfJ9FvTCbE6B2KQwXtm3h" },
      ],
    });
  });

  test("failed, stopped and event-only variants keep their reason", () => {
    const failed = taskNotificationTimelineItem({
      ...agentDone,
      title:
        'Monitor "Perpetuo main run 8b3e594 (cold cache) completion" script failed (exit 1)',
      body: "MAIN RUN DONE: 36500153691|completed|success",
      status: "failed",
      task_tokens: undefined,
      task_tool_uses: undefined,
      task_duration_ms: undefined,
    });
    expect(failed).toMatchObject({
      kindLabel: "Monitor",
      title: "Perpetuo main run 8b3e594 (cold cache) completion",
      detail: "Script failed (exit 1)",
      statusLabel: "Failed",
      tone: "danger",
      facts: [],
    });
    const killed = taskNotificationTimelineItem({
      ...agentDone,
      title:
        'Background command "Serve video folder" was stopped because the system is running low on memory',
      body: "",
      status: "killed",
    });
    expect(killed).toMatchObject({
      kindLabel: "Command",
      title: "Serve video folder",
      detail: "Was stopped because the system is running low on memory",
      statusLabel: "Stopped",
      tone: "attention",
      preview: "",
    });
    const event = taskNotificationTimelineItem({
      ...agentDone,
      title: 'Monitor event: "Warm-cache rerun"',
      body: "WARM RERUN DONE: completed|success",
      status: undefined,
    });
    expect(event).toMatchObject({
      kindLabel: "Monitor",
      title: "Warm-cache rerun",
      detail: undefined,
      status: "event",
      statusLabel: "Update",
      tone: "neutral",
    });
    expect(
      taskNotificationTimelineItem({ ...agentDone, title: "Something else" }),
    ).toMatchObject({ kindLabel: "Background task", title: "Something else" });
  });

  test("ignores other status events", () => {
    expect(
      taskNotificationTimelineItem({ ...agentDone, source: "work_result" }),
    ).toBeNull();
    expect(
      taskNotificationTimelineItem({ ...agentDone, kind: "user_message" }),
    ).toBeNull();
  });

  test("formats usage and previews", () => {
    expect(formatTaskDuration(800)).toBe("1s");
    expect(formatTaskDuration(59_400)).toBe("59s");
    expect(formatTaskDuration(120_000)).toBe("2m");
    expect(formatTaskDuration(3_725_000)).toBe("1h 2m");
    expect(formatTaskTokens(999)).toBe("999");
    expect(formatTaskTokens(53_095)).toBe("53.1k");
    expect(formatTaskTokens(250_400)).toBe("250k");
    expect(formatTaskTokens(1_200_000)).toBe("1.2M");
    expect(markdownPreview("```sh\nls\n```\n\n## Title\n\nbody", 50)).toBe("Title");
    expect(markdownPreview("x".repeat(30), 10)).toBe(`${"x".repeat(9)}…`);
  });
});

describe("task notification in conversation surfaces", () => {
  test("wire normalization keeps task fields", () => {
    const conversation = normalizeCodexConversation({
      available: true,
      events: [{ ...agentDone, task_tokens: "bad" }],
    });
    expect(conversation.events[0]).toMatchObject({
      source: "task_notification",
      task_id: "a3e33bf1f19bac0f0",
      task_tokens: undefined,
      task_tool_uses: 10,
      task_duration_ms: 121781,
    });
  });

  test("timeline renders a card between messages, never a user bubble", () => {
    const items = buildTimeline([
      {
        id: "u1",
        seq: 1,
        kind: "user_message",
        role: "user",
        body: "check the names",
        timestamp: "2026-10-07T01:30:00Z",
      },
      agentDone,
      {
        id: "a1",
        seq: 747800,
        kind: "assistant_message",
        role: "assistant",
        body: "The name is weak.",
        timestamp: "2026-10-07T01:39:20Z",
      },
    ]);
    expect(items.map((item) => item.type)).toEqual([
      "message",
      "task-notification",
      "message",
    ]);
    const userBodies = items.flatMap((item) =>
      item.type === "message" && item.role === "user" ? [item.body] : [],
    );
    expect(userBodies).toEqual(["check the names"]);
  });

  test("a notification never clears a pending user message", () => {
    const pending = [
      {
        id: "local-1",
        lifecycle: "pending" as const,
        dispatchRequestId: "receipt-1",
        dispatchAttemptOrder: 1,
        createdAfterEventIds: [],
      },
    ];
    const result = reconcilePendingUserMessagesAgainstEvents(pending, [
      { id: agentDone.id, seq: agentDone.seq, kind: agentDone.kind },
    ]);
    expect(result.pendingUserMessages).toEqual(pending);
    expect(result.providerEventAliases).toEqual([]);
    // Control: the provider's real user echo does clear it.
    expect(
      reconcilePendingUserMessagesAgainstEvents(pending, [
        { id: agentDone.id, seq: agentDone.seq, kind: agentDone.kind },
        { id: "echo", seq: agentDone.seq + 1, kind: "user_message" },
      ]).pendingUserMessages,
    ).toEqual([]);
  });

  test("semantic equality tracks rendered fields", () => {
    const left = taskNotificationTimelineItem(agentDone)!;
    expect(
      timelineItemsSemanticEqual(left, taskNotificationTimelineItem(agentDone)!),
    ).toBe(true);
    expect(
      timelineItemsSemanticEqual(
        left,
        taskNotificationTimelineItem({ ...agentDone, body: "changed" })!,
      ),
    ).toBe(false);
  });
});
