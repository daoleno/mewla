import { describe, expect, test } from "nsh/test";
import { createChatTimeline } from "../../core/conversation/timeline";

// A thread's rows are projected in one call into JS, and NSH cuts a call off after 2 s ("Error: interrupted": the
// thread stops updating, and can fail the same way each time it opens). Under NSH's own engine, not Bun's JIT, a
// thread as long as a Session sends (240 events) of the heaviest rows agents write projects well inside that: no row
// costs more for a longer command, input or output (core/timeline/rowBounds.ts). It took about 0.3 s on a dev box,
// where the unbounded rows were interrupted.

const at = (index: number) => new Date(1_700_000_000_000 + index * 1000).toISOString();
const lines = (chars: number) =>
  Array.from({ length: Math.ceil(chars / 50) }, (_, index) => `line ${index}: ok some output text here padding....`).join("\n");
const command = (index: number, text: string, body: string) => ({
  id: `c${index}`, seq: index, kind: "command", command: text, body, status: "done", exit_code: 0, timestamp: at(index),
});
const tool = (index: number, name: string, input: unknown, output: string) => ({
  id: `t${index}`, seq: index, kind: "tool", title: "Tool", tool_name: name, input: JSON.stringify(input), output, status: "done", timestamp: at(index),
});

const heaviest = [
  (index: number) => command(index, `bash -lc '${"echo hi; ".repeat(555)}'`, lines(8_000)),
  (index: number) => command(index, `gh pr create --title x --body "${"word ".repeat(1_000)}"`, lines(2_000)),
  (index: number) => command(index, `cat > notes.md <<'EOF'\n${lines(30_000)}\nEOF`, ""),
  (index: number) => tool(index, "Write", { file_path: "/home/me/notes.md", content: lines(30_000) }, "File created successfully"),
  (index: number) => tool(index, "Read", { file_path: "/home/me/notes.md" }, lines(8_000)),
  (index: number) => ({ id: `m${index}`, seq: index, kind: "assistant_message", body: `Done. ${"word ".repeat(40)}`, timestamp: at(index) }),
];

describe("a long thread of heavy rows", () => {
  test("projects inside one call's budget", () => {
    const events = Array.from({ length: 240 }, (_, index) => heaviest[index % heaviest.length](index));
    const rows = createChatTimeline("session")({ conversation: { events }, pendingUserMessages: [] } as never);
    expect(rows.length).toBeGreaterThan(0);
  });
});
