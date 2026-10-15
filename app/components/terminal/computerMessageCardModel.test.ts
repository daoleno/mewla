import { describe, expect, test } from "bun:test";
import { normalizeCodexConversation, type CodexConversationEvent } from "../../services/codexConversation";
import type { StoredServer } from "../../services/storage";
import { buildTimeline, buildTimelineFromSortedEvents } from "./InterfaceTimelineModel";
import { computerMessageTimelineItem, serverForComputer } from "./computerMessageCardModel";

const MAC_ID = "fdce8164daf8e28d2e8a9ca286925ccb69d9eca64b80795e3dacf771c46d3f35";

function event(computer: CodexConversationEvent["computer"], body = "Reply on GitHub: 收到，明天看"): CodexConversationEvent {
  return { id: "computer-handoff:m_1", seq: 1, kind: "status", role: "assistant", source: "computer_message", body, computer };
}

describe("computer message cards", () => {
  test("a task handed over shows where it went and whether it arrived", () => {
    const item = computerMessageTimelineItem(
      event({ direction: "to", computer: "Mac", daemon_id: MAC_ID, message_id: "m_1", kind: "request", state: "delivered", user_said: "在 Mac 上回复" }),
    );
    expect(item).toMatchObject({
      type: "computer-message",
      kindLabel: "Handed to Mac",
      statusLabel: "Delivered",
      tone: "accent",
      userSaid: "在 Mac 上回复",
      body: "Reply on GitHub: 收到，明天看",
    });
  });

  test("an answer reads as the other computer's, with no delivery state", () => {
    const item = computerMessageTimelineItem(
      event({ direction: "from", computer: "Mac", message_id: "m_9", kind: "reply", state: "delivered" }),
    );
    expect(item?.kindLabel).toBe("Reply from Mac");
    expect(item?.statusLabel).toBeUndefined();
  });

  test("a message that may not have arrived says so", () => {
    expect(
      computerMessageTimelineItem(event({ direction: "to", computer: "Mac", message_id: "m_1", kind: "question", state: "uncertain" })),
    ).toMatchObject({ kindLabel: "Asked Mac", statusLabel: "May not have arrived", tone: "attention" });
  });

  test("the conversation shows the card instead of an activity row", () => {
    const items = buildTimelineFromSortedEvents([
      event({ direction: "to", computer: "Mac", message_id: "m_1", kind: "request", state: "queued" }),
    ]);
    expect(items.map((item) => item.type)).toEqual(["computer-message"]);
  });

  test("Open on finds the other computer by its id, or the start of it", () => {
    const servers = [
      { id: "linux", daemonId: "aa".repeat(32) },
      { id: "mac", daemonId: MAC_ID },
    ] as StoredServer[];
    expect(serverForComputer(servers, MAC_ID, "linux")?.id).toBe("mac");
    expect(serverForComputer(servers, MAC_ID.slice(0, 16), "linux")?.id).toBe("mac");
    expect(serverForComputer(servers, MAC_ID, "mac")).toBeNull();
    expect(serverForComputer(servers, "fdce", "linux")).toBeNull();
    expect(serverForComputer(servers, undefined, "linux")).toBeNull();
  });

  test("the computer fields survive the wire, and a malformed one is dropped", () => {
    const conversation = normalizeCodexConversation({
      available: true,
      events: [
        { id: "a", seq: 1, kind: "status", source: "computer_message", body: "x", computer: { direction: "to", computer: "Mac", message_id: "m_1", kind: "request", state: "sent" } },
        { id: "b", seq: 2, kind: "status", source: "computer_message", body: "y", computer: { direction: "sideways", computer: "Mac", message_id: "m_2" } },
      ],
    });
    expect(conversation?.events[0].computer).toMatchObject({ direction: "to", computer: "Mac", state: "sent" });
    expect(conversation?.events[1].computer).toBeUndefined();
  });

  test("a handover sorts among the conversation's messages by time", () => {
    const items = buildTimeline([
      { id: "late", seq: 2, kind: "assistant_message", role: "assistant", body: "Done", timestamp: "2026-10-15T10:02:00+08:00" },
      { ...event({ direction: "to", computer: "Mac", message_id: "m_1", kind: "request" }), seq: 0, timestamp: "2026-10-15T02:01:00Z" },
      { id: "early", seq: 1, kind: "user_message", role: "user", body: "Reply on the Mac", timestamp: "2026-10-15T10:00:00+08:00" },
    ]);
    expect(items.map((item) => item.id)).toEqual(["early", "computer-handoff:m_1", "late"]);
  });
});
