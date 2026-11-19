import { describe, expect, test } from "bun:test";
import { normalizeCodexConversation } from "../conversation/conversation";
import { buildTimeline } from "./model";

// The daemon's line for a Stop that ended the commands its turn had started
// (source commands_stopped): one status row, under the interrupted turn.
describe("a Stop's commands line", () => {
  const conversation = (status: string, title: string) =>
    normalizeCodexConversation({
      available: true,
      events: [
        { id: "u1", seq: 1, timestamp: "2026-11-19T04:00:00Z", kind: "user_message", role: "user", body: "count to 30" },
        { id: "s1", seq: 2, timestamp: "2026-11-19T04:00:12Z", kind: "status", role: "system", title: "Conversation interrupted", status: "interrupted" },
        { id: "stopped:%1:t1:1", seq: 0, timestamp: "2026-11-19T04:00:11.5Z", kind: "status", role: "system", source: "commands_stopped", title, status },
      ],
    });

  test("is one plain line, in time order", () => {
    const items = buildTimeline(conversation("stopped", "Stopped 1 command this turn started: seq 1 30").events);
    const row = items.find((item) => item.id === "stopped:%1:t1:1");
    expect(row).toMatchObject({ type: "activity", title: "Stopped 1 command this turn started: seq 1 30", tone: "neutral" });
    expect(row && "body" in row ? row.body : undefined).toBeUndefined();
    expect(items.map((item) => item.id).slice(-2)).toEqual(["stopped:%1:t1:1", "s1"]);
  });

  test("a command that could not be stopped reads as a failure", () => {
    const items = buildTimeline(conversation("failed", "1 command could not be stopped: make serve").events);
    expect(items.find((item) => item.id === "stopped:%1:t1:1")).toMatchObject({ type: "activity", tone: "failed", title: "1 command could not be stopped: make serve" });
  });
});
