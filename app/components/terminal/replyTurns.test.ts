import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { replyTurnsOf } from "./InterfaceTimelineModel";

function event(seq: number, kind: string, body: string, extra: Partial<CodexConversationEvent> = {}) {
  return { id: `${kind}-${seq}`, seq, kind, body, ...extra } as CodexConversationEvent;
}

describe("replyTurnsOf", () => {
  test("pairs every turn's reply with the prompt it answers, the newest as latest", () => {
    const turns = replyTurnsOf(
      [
        event(1, "user_message", "first"),
        event(2, "assistant_message", "one"),
        event(3, "user_message", "second"),
        event(4, "command", ""),
        event(5, "assistant_message", "two"),
      ],
      0,
    );
    expect([...turns.keys()]).toEqual(["assistant_message-2", "assistant_message-5"]);
    expect(turns.get("assistant_message-2")).toMatchObject({
      latest: false,
      settled: true,
      prompt: { id: "user_message-1", body: "first" },
    });
    expect(turns.get("assistant_message-5")).toMatchObject({
      latest: true,
      prompt: { id: "user_message-3", body: "second", hasAttachments: false },
    });
  });

  test("a turn with text between tool calls gets its row under the last piece only", () => {
    const turns = replyTurnsOf(
      [
        event(1, "user_message", "go"),
        event(2, "assistant_message", "looking"),
        event(3, "command", ""),
        event(4, "assistant_message", "done"),
      ],
      0,
    );
    expect([...turns.keys()]).toEqual(["assistant_message-4"]);
  });

  test("while a newer prompt is unanswered or a message is sending, nothing is latest or settled", () => {
    const answering = replyTurnsOf(
      [event(1, "user_message", "a"), event(2, "assistant_message", "b"), event(3, "user_message", "c")],
      0,
    );
    expect(answering.get("assistant_message-2")?.latest).toBe(false);
    const sending = replyTurnsOf([event(1, "user_message", "a"), event(2, "assistant_message", "b")], 1);
    expect(sending.get("assistant_message-2")).toMatchObject({ latest: true, settled: false });
  });

  test("a streaming reply has no row yet", () => {
    expect(
      replyTurnsOf([event(1, "user_message", "a"), event(2, "assistant_message", "b", { partial: true })], 0).size,
    ).toBe(0);
  });

  test("a reply with no prompt before it still has a row, with nothing to retry", () => {
    const turns = replyTurnsOf([event(1, "assistant_message", "hello")], 0);
    expect(turns.get("assistant_message-1")).toMatchObject({ prompt: undefined, latest: false });
  });

  test("events arriving out of order are read in conversation order", () => {
    const turns = replyTurnsOf([event(2, "assistant_message", "b"), event(1, "user_message", "a")], 0);
    expect(turns.get("assistant_message-2")?.prompt?.body).toBe("a");
  });
});
