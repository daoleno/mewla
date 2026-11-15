// @ts-nocheck
import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../conversation/conversation";
import { buildTimeline } from "./model";

function assistant(body: string): CodexConversationEvent {
  return {
    id: "assistant-message-7",
    seq: 7,
    kind: "assistant_message",
    role: "assistant",
    body,
  };
}

function tool(
  body: string,
  status: "running" | "done",
): CodexConversationEvent {
  return {
    id: "tool-call-7",
    seq: 7,
    kind: "tool",
    tool_name: "Grep",
    input: `{"pattern":"Tool header","path":"app"}`,
    body,
    status,
    partial: status === "running",
  };
}

describe("timeline identity", () => {
  test("streaming content changes retain the daemon event key", () => {
    const partial = buildTimeline([assistant("partial")]);
    const complete = buildTimeline([assistant("complete response")]);

    expect(partial[0]?.id).toBe("assistant-message-7");
    expect(complete[0]?.id).toBe(partial[0]?.id);
  });

  test("provider echo keeps canonical content while carrying its causal turn alias", () => {
    const providerEcho = {
      id: "provider-user-9",
      seq: 9,
      kind: "user_message",
      role: "user",
      body: "provider canonical body",
    } as CodexConversationEvent;
    const items = buildTimeline(
      [providerEcho],
      new Map([[providerEcho.id, "pending-current"]]),
    );

    expect(items[0]).toMatchObject({
      id: "provider-user-9",
      body: "provider canonical body",
      turnFocusAnchorId: "pending-current",
    });
  });

  test("same-logical-Tool streaming upserts retain the mounted row key", () => {
    const running = buildTimeline([tool("first match", "running")]);
    const done = buildTimeline([tool("first match\nsecond match", "done")]);

    expect(running[0]?.type).toBe("activity");
    expect(done[0]?.type).toBe("activity");
    expect(running[0]?.id).toBe("tool-call-7");
    expect(done[0]?.id).toBe(running[0]?.id);
  });
});
