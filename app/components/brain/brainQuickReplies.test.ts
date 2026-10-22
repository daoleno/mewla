import { describe, expect, test } from "bun:test";
import { latestUserMessageAt } from "../terminal/latestUserMessageAt";
import { quickRepliesVisible } from "./brainQuickReplies";

const replies = { rows: [["1 处理", "1 不是问题", "1 稍后"]], at: "2026-10-22T12:00:00Z" };
const at = Date.parse(replies.at);

describe("quick replies", () => {
  test("show under Brain's last message while the person has not written", () => {
    expect(quickRepliesVisible(replies, { draft: "", lastUserMessageAt: 0, hasMessage: true })).toBe(true);
    expect(quickRepliesVisible(replies, { draft: "", lastUserMessageAt: at - 1000, hasMessage: true })).toBe(true);
  });

  test("need Brain's message on screen: no message, no chips", () => {
    expect(quickRepliesVisible(replies, { draft: "", lastUserMessageAt: 0, hasMessage: false })).toBe(false);
    expect(quickRepliesVisible(replies, { draft: "", lastUserMessageAt: 0, hasMessage: true })).toBe(true);
  });

  test("go once the person writes after them, or is writing", () => {
    expect(quickRepliesVisible(replies, { draft: "", lastUserMessageAt: at + 1000, hasMessage: true })).toBe(false);
    expect(quickRepliesVisible(replies, { draft: " 1 ", lastUserMessageAt: 0, hasMessage: true })).toBe(false);
    expect(quickRepliesVisible(undefined, { draft: "", lastUserMessageAt: 0, hasMessage: true })).toBe(false);
    expect(quickRepliesVisible({ rows: [], at: replies.at }, { draft: "", lastUserMessageAt: 0, hasMessage: true })).toBe(false);
  });

  test("a message still being delivered counts as the reply", () => {
    expect(latestUserMessageAt([], [{}])).toBe(Number.MAX_SAFE_INTEGER);
    expect(quickRepliesVisible(replies, { draft: "", lastUserMessageAt: latestUserMessageAt([], [{}]), hasMessage: true })).toBe(false);
  });

  test("Watch's own events are not the person speaking", () => {
    const events = [
      { id: "u1", role: "user", timestamp: "2026-10-22T11:00:00Z" },
      { id: "watch:brief:abc", role: "user", timestamp: "2026-10-22T12:30:00Z" },
      { id: "a1", role: "assistant", timestamp: "2026-10-22T12:31:00Z" },
    ];
    expect(latestUserMessageAt(events, [])).toBe(Date.parse("2026-10-22T11:00:00Z"));
  });
});
