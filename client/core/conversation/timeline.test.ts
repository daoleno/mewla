import { describe, expect, test } from "bun:test";
import { SCREENSHOT_CHAT_ACTIVITY_EVENTS } from "../timeline/chatActivityFixtures";
import type { CodexConversationEvent } from "./conversation";
import { initialInterfaceChatThreadState, type InterfaceChatThreadState, type PendingUserMessage } from "./thread";
import { createChatTimeline, isWorkingRow } from "./timeline";

function state(events: CodexConversationEvent[], extra: Partial<InterfaceChatThreadState> = {}, running = false): InterfaceChatThreadState {
  return {
    ...initialInterfaceChatThreadState("test:timeline"),
    loading: false,
    conversation: { available: true, events, activity: running ? { id: "turn-1", status: "running" } as never : undefined },
    ...extra,
  };
}

const pending = (id: string, lifecycle: PendingUserMessage["lifecycle"]): PendingUserMessage => ({
  id, body: "and then?", sentText: "and then?", attachments: [], createdAt: "2026-06-18T09:31:00.000Z", lifecycle,
  dispatchRequestId: `req-${id}`, dispatchAttemptOrder: 1, createdAfterEventIds: ["activity-assistant"],
});

describe("chat timeline", () => {
  test("a Session's finished tool run folds into one row; messages stay", () => {
    const items = createChatTimeline("session")(state(SCREENSHOT_CHAT_ACTIVITY_EVENTS));
    expect(items[0]).toMatchObject({ type: "message", role: "user" });
    expect(items.some((item) => item.type === "message" && item.role === "assistant")).toBe(true);
    const activities = items.filter((item) => item.type === "activity");
    expect(activities.length).toBeLessThan(SCREENSHOT_CHAT_ACTIVITY_EVENTS.filter((e) => e.kind !== "user_message" && e.kind !== "assistant_message").length);
  });

  test("Working shows while a reply runs, not while a question waits", () => {
    const timeline = createChatTimeline("brain");
    const events = SCREENSHOT_CHAT_ACTIVITY_EVENTS.slice(0, 1);
    expect(timeline(state(events, {}, true)).some(isWorkingRow)).toBe(true);
    const asking: CodexConversationEvent = {
      id: "ask", seq: 2, call_id: "ask", kind: "tool", tool_name: "AskUserQuestion", status: "running", timestamp: "2026-06-18T09:30:01.000Z",
      choice: { call_id: "ask", state: "pending", questions: [{ question: "Color?", options: [{ label: "Blue" }] }] },
    } as never;
    expect(timeline(state([...events, asking], {}, true)).some(isWorkingRow)).toBe(false);
  });

  test("what was sent shows after the reply it follows; a failed one can be sent again", () => {
    const actions: string[] = [];
    const items = createChatTimeline("session")(state(SCREENSHOT_CHAT_ACTIVITY_EVENTS, { pendingUserMessages: [pending("p1", "failed")] }), {
      onPendingAction: (id, action) => actions.push(`${id}:${action}`),
    });
    const at = items.findIndex((item) => item.id === "p1");
    const reply = items.findIndex((item) => item.id === "activity-assistant");
    expect(at).toBeGreaterThan(reply);
    const row = items[at];
    if (row.type !== "message") throw new Error("not a message");
    row.onRetryPending?.();
    expect(actions).toEqual(["p1:retry"]);
  });

  test("unchanged rows keep their objects; a streamed reply replaces only its own", () => {
    const timeline = createChatTimeline("session");
    const first = timeline(state(SCREENSHOT_CHAT_ACTIVITY_EVENTS));
    expect(timeline(state(SCREENSHOT_CHAT_ACTIVITY_EVENTS))).toBe(first);
    const events = SCREENSHOT_CHAT_ACTIVITY_EVENTS.map((e) => (e.id === "activity-assistant" ? { ...e, body: `${e.body} More.` } : e));
    const next = timeline(state(events));
    expect(next).not.toBe(first);
    for (const [index, item] of next.entries()) {
      if (item.id === "activity-assistant") expect(item).not.toBe(first[index]);
      else if (item.type === "message") expect(item).toBe(first.find((f) => f.id === item.id)!);
    }
  });
});
