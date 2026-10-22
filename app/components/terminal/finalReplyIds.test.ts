import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { finalReplyIdsOf } from "./InterfaceTimelineModel";

function event(seq: number, kind: string, body: string, extra: Partial<CodexConversationEvent> = {}) {
  return { id: `${kind}-${seq}`, seq, kind, body, ...extra } as CodexConversationEvent;
}

describe("finalReplyIdsOf", () => {
  test("every turn's final reply, older turns included", () => {
    const ids = finalReplyIdsOf([
      event(1, "user_message", "first"),
      event(2, "assistant_message", "one"),
      event(3, "user_message", "second"),
      event(4, "command", ""),
      event(5, "assistant_message", "two"),
    ]);
    expect([...ids]).toEqual(["assistant_message-2", "assistant_message-5"]);
  });

  test("a turn with text between tool calls gets its row under the last piece only", () => {
    const ids = finalReplyIdsOf([
      event(1, "user_message", "go"),
      event(2, "assistant_message", "looking"),
      event(3, "command", ""),
      event(4, "assistant_message", "done"),
    ]);
    expect([...ids]).toEqual(["assistant_message-4"]);
  });

  test("a streaming reply has no row yet", () => {
    expect(
      finalReplyIdsOf([event(1, "user_message", "a"), event(2, "assistant_message", "b", { partial: true })]).size,
    ).toBe(0);
  });

  test("a reply with no prompt before it still has a row", () => {
    expect(finalReplyIdsOf([event(1, "assistant_message", "hello")]).has("assistant_message-1")).toBe(true);
  });

  test("events arriving out of order are read in conversation order", () => {
    const ids = finalReplyIdsOf([event(2, "assistant_message", "b"), event(1, "user_message", "a")]);
    expect([...ids]).toEqual(["assistant_message-2"]);
  });
});
