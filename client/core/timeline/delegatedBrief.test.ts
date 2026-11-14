import { describe, expect, test } from "bun:test";
import { delegatedTask } from "./delegatedBrief";
import { buildTimeline } from "./model";
import type { CodexConversationEvent } from "../conversation/conversation";

// The body the daemon projects for a delegated Claude brief (daemon TestClaudeDelegatedBriefIsMarkedDelegated).
const PROTOCOL = "Mewla lifecycle protocol:\nProfile: quick.\nYou are the delegated Worker.";
const BRIEF = `Execute:\n\nCreate hello.txt with one line: hello\n\n${PROTOCOL}`;

describe("a delegated Worker's brief", () => {
  test("marked delegated: the task alone, without the intent or the protocol", () => {
    expect(delegatedTask({ body: BRIEF, origin: "delegated" })).toBe("Create hello.txt with one line: hello");
    expect(delegatedTask({ body: "Ship the docs.\n\nKeep the links.", origin: "delegated" })).toBe("Ship the docs.\n\nKeep the links.");
    expect(delegatedTask({ body: `Execute:\n\n${PROTOCOL}`, origin: "delegated" })).toBe("");
  });

  test("unmarked: only the strict old shape counts; the person's own words stay theirs", () => {
    expect(delegatedTask({ body: `Execute: Create hello.txt\n\n${PROTOCOL}` })).toBe("Create hello.txt");
    expect(delegatedTask({ body: "Execute: the plan we agreed" })).toBeNull();
    expect(delegatedTask({ body: `Make hello.txt\n\n${PROTOCOL}` })).toBeNull();
    expect(delegatedTask({ body: "Execute: tell me about the Mewla lifecycle protocol: what is it?" })).toBeNull();
    expect(delegatedTask({ body: "hi", origin: "provider" })).toBeNull();
  });

  test("the timeline row carries the task; a brief with no task is no row", () => {
    const event = (id: string, seq: number, body: string, origin?: string) =>
      ({ id, seq, kind: "user_message", body, origin, timestamp: `2026-11-12T17:29:0${seq}Z` }) as CodexConversationEvent;
    const items = buildTimeline([event("b", 1, BRIEF, "delegated"), event("p", 2, `Execute:\n\n${PROTOCOL}`, "delegated"), event("u", 3, "thanks")]);
    expect(items.map((item) => item.id)).toEqual(["b", "u"]);
    expect(items[0]).toMatchObject({ type: "message", role: "user", delegatedTask: "Create hello.txt with one line: hello" });
    expect(items[1]).not.toHaveProperty("delegatedTask");
  });
});
