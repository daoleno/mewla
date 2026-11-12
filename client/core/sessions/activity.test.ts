import { describe, expect, test } from "bun:test";
import { elapsedShort, sessionActivity } from "./activity";
import type { Worker } from "./workers";

const FOOTER = ["⏸ manual mode on · ? for shortcuts"];
const PASTED = ['"$MEWLA_WORKER_PROGRESS_CMD" worker progress --turn-id t1 --status running'];
const worker = (patch: Partial<Worker>): Worker => ({
  key: "s:w", id: "w", serverId: "s", serverName: "Desk", serverUrl: "ws://x", name: "w", status: "running", summary: "", last_output_lines: [], ...patch,
});

describe("a Session's line says what it is doing in words", () => {
  test("never the terminal: its footer and a pasted prompt fall to the state word", () => {
    expect(sessionActivity(worker({ status: "unknown", summary: FOOTER[0]!, last_output_lines: FOOTER }))).toBe("Idle");
    expect(sessionActivity(worker({ status: "running", summary: PASTED[0]!, last_output_lines: PASTED, delegated: true }))).toBe("Working");
    expect(sessionActivity(worker({ status: "done", last_output_lines: FOOTER }))).toBe("Done");
    expect(sessionActivity(worker({ status: "failed", summary: "Error: exit 1" }))).toBe("Failed");
  });

  test("what it reported, while it runs and once it is done", () => {
    expect(sessionActivity(worker({ status: "running", phase: "working", summary: "Building the comparison table", last_output_lines: FOOTER }))).toBe("Building the comparison table");
    expect(sessionActivity(worker({ status: "done", event_kind: "done", summary: "Merged, CI green" }))).toBe("Merged, CI green");
    // The screen's last line after an old report is still the screen's.
    expect(sessionActivity(worker({ status: "unknown", phase: "working", summary: FOOTER[0]! }))).toBe("Idle");
  });

  test("a reported phase without a summary is its word", () => {
    expect(sessionActivity(worker({ status: "running", phase: "verifying" }))).toBe("Verifying");
  });

  test("what it asks comes first", () => {
    expect(sessionActivity(worker({ status: "blocked", approvals: [{ id: "a", tool: "Bash", summary: "Run git push?\nmore", options: [] }] }))).toBe("Run git push?");
    expect(sessionActivity(worker({ status: "blocked", prompt: { id: "p", kind: "permission", request: "\n rm -rf build\n", options: [] } as never }))).toBe("rm -rf build");
    expect(sessionActivity(worker({ status: "blocked", needs_attention: true, summary: FOOTER[0]! }))).toBe("Waiting for you");
  });
});

describe("elapsed time at the row's end", () => {
  const now = Date.UTC(2026, 10, 12, 12, 0);
  test("minutes, hours, days; at least a minute; nothing without a start", () => {
    expect(elapsedShort(now - 5_000, now)).toBe("1m");
    expect(elapsedShort(now - 12 * 60_000, now)).toBe("12m");
    expect(elapsedShort(now - 3 * 3_600_000 - 59_000, now)).toBe("3h");
    expect(elapsedShort(now - 50 * 3_600_000, now)).toBe("2d");
    expect(elapsedShort(undefined, now)).toBe("");
  });
});
