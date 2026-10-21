import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { latestReplyOf } from "./InterfaceTimelineModel";

function event(seq: number, kind: string, body: string, extra: Partial<CodexConversationEvent> = {}) {
  return { id: `${kind}-${seq}`, seq, kind, body, ...extra } as CodexConversationEvent;
}

describe("latestReplyOf", () => {
  test("pairs the newest finished reply with the prompt it answers", () => {
    const events = [
      event(1, "user_message", "first"),
      event(2, "assistant_message", "one"),
      event(3, "user_message", "second"),
      event(4, "command", ""),
      event(5, "assistant_message", "two"),
    ];
    const result = latestReplyOf(events, 0);
    expect(result?.replyId).toBe("assistant_message-5");
    expect(result?.prompt).toMatchObject({ id: "user_message-3", body: "second", hasAttachments: false });
  });

  test("a reply that a newer prompt follows, a streaming reply or a pending send offers no retry", () => {
    expect(latestReplyOf([event(1, "user_message", "a"), event(2, "assistant_message", "b"), event(3, "user_message", "c")], 0)).toBeUndefined();
    expect(latestReplyOf([event(1, "user_message", "a"), event(2, "assistant_message", "b", { partial: true })], 0)).toBeUndefined();
    expect(latestReplyOf([event(1, "user_message", "a"), event(2, "assistant_message", "b")], 1)).toBeUndefined();
  });

  test("a reply with no prompt before it has nothing to retry", () => {
    expect(latestReplyOf([event(1, "assistant_message", "hello")], 0)).toBeUndefined();
  });

  test("events arriving out of order are read in conversation order", () => {
    const result = latestReplyOf([event(2, "assistant_message", "b"), event(1, "user_message", "a")], 0);
    expect(result?.prompt.body).toBe("a");
  });
});
