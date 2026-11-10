// @ts-nocheck
import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../conversation/conversation";
import {
  buildTimeline,
} from "./model";

function assistant(
  id: string,
  seq: number,
  body: string,
): CodexConversationEvent {
  return {
    id,
    seq,
    kind: "assistant_message",
    role: "assistant",
    body,
    timestamp: `2026-08-04T00:00:0${seq}.000Z`,
  };
}

describe("timeline history viewport stability", () => {
  test("canonical append retains history item identity", () => {
    const before = buildTimeline([
      assistant("oldest", 0, "oldest"),
      assistant("history-anchor", 2, "settled history"),
    ]);
    const after = buildTimeline([
      ...[
        assistant("oldest", 0, "oldest"),
        assistant("history-anchor", 2, "settled history"),
      ],
      assistant("newest", 4, "live append"),
    ]);

    expect(after.find((item) => item.id === "history-anchor")?.id).toBe(
      before.find((item) => item.id === "history-anchor")?.id,
    );
    expect(after).not.toBe(before);
  });

  test("same-ID streaming update keeps the mounted item key", () => {
    const partial = buildTimeline([
      assistant("history-anchor", 1, "history"),
      assistant("streaming-assistant", 2, "partial"),
    ]);
    const complete = buildTimeline([
      assistant("history-anchor", 1, "history"),
      assistant(
        "streaming-assistant",
        2,
        "complete response with enough content to grow the row",
      ),
    ]);

    expect(complete.map((item) => item.id)).toEqual(
      partial.map((item) => item.id),
    );
    expect(complete).not.toBe(partial);
  });
});
