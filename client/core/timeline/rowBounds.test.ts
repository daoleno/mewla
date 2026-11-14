import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../conversation/conversation";
import { buildTimeline } from "./model";
import { ROW_SCAN_HEAD, codePointLength, commandHead, scanHead, textWindow } from "./rowBounds";
import { parseExecWrapperCalls } from "./toolCallSemantics";

type Row = { title?: string; semanticKind?: string; body?: string; commandText?: string };

const commandRow = (command: string, body = "") =>
  buildTimeline([
    { id: "c1", seq: 1, kind: "command", command, body, status: "done", exit_code: 0, timestamp: "2026-11-14T00:00:00Z" } as CodexConversationEvent,
  ])[0] as Row;

const toolRow = (toolName: string, input: unknown, output = "") =>
  buildTimeline([
    { id: "t1", seq: 1, kind: "tool", title: "Tool", tool_name: toolName, input: JSON.stringify(input), output, status: "done", timestamp: "2026-11-14T00:00:00Z" } as CodexConversationEvent,
  ])[0] as Row;

const lines = (count: number) => Array.from({ length: count }, (_, index) => `line ${index}: ok`).join("\n");
const HINT = "Expand this item for full output.";

describe("what a row reads of a command", () => {
  test("a short command whole, a long one's head", () => {
    expect(scanHead("ls -la")).toBe("ls -la");
    expect(scanHead("x".repeat(ROW_SCAN_HEAD * 3))).toHaveLength(ROW_SCAN_HEAD);
  });

  test("never half a surrogate pair", () => {
    const head = scanHead(`${"x".repeat(ROW_SCAN_HEAD - 1)}🙂🙂`);
    expect(head).toBe("x".repeat(ROW_SCAN_HEAD - 1));
  });

  test("a quote the head cuts off is closed again", () => {
    expect(commandHead(`gh pr create --body "${"word ".repeat(500)}"`).endsWith('"')).toBe(true);
    expect(commandHead(`python3 -c '${"x = 1; ".repeat(200)}'`).endsWith("'")).toBe(true);
    expect(commandHead(`echo "a \\" b" ${"y".repeat(ROW_SCAN_HEAD)}`).endsWith("y")).toBe(true);
  });

  test("a long command's row reads as the short one's, and keeps the whole command", () => {
    const body = "word ".repeat(1_000);
    const long = commandRow(`gh pr create --title x --body "${body}"`);
    expect(long.title).toBe(commandRow('gh pr create --title x --body "word word word word word word word word word word word word word word"').title);
    expect(long.commandText).toBe(`gh pr create --title x --body "${body}"`);
    const heredoc = commandRow(`cat > notes.md <<'EOF'\n${lines(3_000)}\nEOF`);
    expect(heredoc.title).toBe(commandRow("cat > notes.md <<'EOF'\nhi\nEOF").title);
    expect(heredoc.semanticKind).toBe(commandRow("cat > notes.md <<'EOF'\nhi\nEOF").semanticKind);
  });

  test("a file write's row reads as a short one's", () => {
    const short = toolRow("Write", { file_path: "/home/me/notes.md", content: "hi" }, "File created");
    const long = toolRow("Write", { file_path: "/home/me/notes.md", content: lines(3_000) }, "File created");
    expect(long.title).toBe(short.title);
    expect(long.semanticKind).toBe(short.semanticKind);
  });

  test("an input with no tools.NAME( call has no exec calls, whatever its size", () => {
    expect(parseExecWrapperCalls(JSON.stringify({ content: lines(3_000) }))).toEqual([]);
    expect(parseExecWrapperCalls('await tools.view_image({path: "/tmp/a.png"});').map((call) => call.name)).toEqual(["view_image"]);
  });
});

describe("what a row reads of an output", () => {
  test("a short text whole", () => {
    expect(textWindow("a\nb")).toEqual({ text: "a\nb", omitted: 0 });
  });

  test("a long text's start and end in whole lines, counting what it leaves out", () => {
    const text = lines(5_000);
    const window = textWindow(text);
    expect(window.text.startsWith("line 0: ok\n")).toBe(true);
    expect(window.text.endsWith("line 4999: ok")).toBe(true);
    for (const line of window.text.split("\n")) expect(line).toMatch(/^line \d+: ok$/);
    expect(codePointLength(window.text) + window.omitted).toBe(codePointLength(text));
  });

  test("counts characters as the preview does, a surrogate pair as one", () => {
    const text = "🙂".repeat(20_000);
    const window = textWindow(text);
    expect(window.text.includes("�")).toBe(false);
    expect(codePointLength(window.text) + window.omitted).toBe(20_000);
  });

  test("the preview of a long output shows its start and end and how much is hidden", () => {
    expect(commandRow("go test ./...", "x".repeat(30_000)).body).toBe(
      `${"x".repeat(696)}\n... 28880 chars hidden. ${HINT}\n${"x".repeat(424)}`,
    );
    expect(commandRow("go test ./...", "🙂".repeat(30_000)).body).toBe(
      `${"🙂".repeat(696)}\n... 28880 chars hidden. ${HINT}\n${"🙂".repeat(424)}`,
    );
  });

  test("a long JSON output after a transport header is still compacted whole", () => {
    const json = JSON.stringify({ items: Array.from({ length: 800 }, (_, id) => ({ id, name: `item ${id}` })) }, null, 2);
    const compact = JSON.stringify(JSON.parse(json)).replace(/":/g, '": ').replace(/,"/g, ', "');
    const body = commandRow("go test ./...", `Chunk ID: ab12\nWall time: 1.2 seconds\nProcess exited with code 0\nOutput:\n${json}`).body ?? "";
    expect(body.startsWith(compact.slice(0, 600))).toBe(true);
    expect(body).toContain(`... ${codePointLength(compact) - 696 - 424} chars hidden.`);
  });
});
