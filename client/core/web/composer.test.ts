import { describe, expect, test } from "bun:test";
import { lastEditablePrompt } from "./composer";

const message = (id: string, role: "user" | "assistant", patch: Record<string, unknown> = {}) => ({ type: "message", id, role, body: `text ${id}`, attachments: [], ...patch });
const idle = { connected: true, running: false };

describe("↑ edits the last message", () => {
  test("the newest prompt of the person, past replies and other rows", () => {
    const items = [message("u1", "user"), message("a1", "assistant"), message("u2", "user"), message("a2", "assistant"), { type: "plan", id: "p" }];
    expect(lastEditablePrompt(items, idle)?.id).toBe("u2");
  });

  test("never reaches past a newest prompt that cannot be edited", () => {
    expect(lastEditablePrompt([message("u1", "user"), message("u2", "user", { pending: true })], idle)).toBeNull();
    expect(lastEditablePrompt([message("u1", "user"), message("u2", "user", { attachments: [{ name: "a.png" }] })], idle)).toBeNull();
  });

  test("nothing while a turn runs, offline, or with no prompt yet", () => {
    const items = [message("u1", "user")];
    expect(lastEditablePrompt(items, { connected: true, running: true })).toBeNull();
    expect(lastEditablePrompt(items, { connected: false, running: false })).toBeNull();
    expect(lastEditablePrompt([message("a1", "assistant")], idle)).toBeNull();
  });
});
