import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../conversation/conversation";
import { projectTimeline } from "./projectTimeline";
import { primarySemanticAction } from "./toolCallSemantics";

const at = (i: number) => new Date(Date.UTC(2026, 10, 14, 6, 0, i)).toISOString();
const command = (i: number): CodexConversationEvent =>
  ({ id: `c${i}`, seq: i, kind: "command", command: `go test ./server -run TestCase${i} -count=1`, output: "ok", body: "Exit code 0", status: "done", exit_code: 0, timestamp: at(i) }) as CodexConversationEvent;
const reply = (i: number): CodexConversationEvent =>
  ({ id: `m${i}`, seq: i, kind: "assistant_message", role: "assistant", body: `Reply ${i}.`, timestamp: at(i) }) as CodexConversationEvent;

describe("each event is projected once (owner 11-14: every page of an opening thread projected all of it again)", () => {
  test("a page of older events before a command projects the thread anew, and the command rows it had are the same objects", () => {
    const newer = [command(41), reply(42), command(43), reply(44)];
    const first = projectTimeline(newer, null);
    const older = [reply(1), command(2), reply(3), command(4)];
    const both = projectTimeline([...older, ...newer], first.cache);
    const commandRows = first.items.filter((row) => row.type === "activity");
    expect(commandRows.length).toBe(2);
    for (const row of commandRows) expect(both.items).toContain(row);
    expect(both.items.length).toBe(first.items.length + 4);
  });

  test("a call's action is read once per what it says, and is the same action every time", () => {
    const call = { kind: "command", command: "rg -n sessionRowModel client/core | head -20", toolName: "exec_command", status: "done", exitCode: 0 };
    const action = primarySemanticAction(call);
    expect(primarySemanticAction({ ...call })).toBe(action);
    expect(primarySemanticAction({ ...call, status: "failed", exitCode: 1 })).not.toBe(action);
  });
});
