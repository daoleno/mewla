import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../conversation/conversation";
import { buildTimeline } from "./model";

const row = (command: string) =>
  buildTimeline([
    { id: "c1", seq: 1, kind: "command", command, status: "done", exit_code: 0, timestamp: "2026-11-14T00:00:00Z" } as CodexConversationEvent,
  ])[0] as { title?: string; semanticKind?: string };

describe("a command row with a very long first line", () => {
  // A row reads only the head of its command's first line (COMMAND_HEAD in model.ts): every row is projected
  // again on each change of its thread, and on iOS long one-line commands took that past NSH's 2 s call budget.
  test("still names what it runs from the line's head", () => {
    const short = row("cat /tmp/notes.md");
    const long = row(`cat /tmp/notes.md ${"x".repeat(5_000)}`);
    expect(short.title).toBe("Read notes.md");
    expect(long.title).toBe(short.title);
    expect(long.semanticKind).toBe(short.semanticKind);
  });
});
