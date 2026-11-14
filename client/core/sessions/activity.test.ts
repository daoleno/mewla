import { describe, expect, test } from "bun:test";
import { elapsedShort, promptTitle, sessionActivity, workingLineShown, workingSince } from "./activity";
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

describe("elapsed time, one form everywhere (SPEC §11 Elapsed time)", () => {
  const now = Date.UTC(2026, 10, 12, 12, 0);
  test("seconds, minutes, hours and minutes, days and hours; never a clock; nothing without a start", () => {
    expect(elapsedShort(now - 16_000, now)).toBe("16s");
    expect(elapsedShort(now - 12 * 60_000 - 30_000, now)).toBe("12m");
    expect(elapsedShort(now - (2 * 60 + 57) * 60_000, now)).toBe("2h 57m");
    expect(elapsedShort(now - 3 * 3_600_000, now)).toBe("3h");
    expect(elapsedShort(now - 27 * 3_600_000, now)).toBe("1d 3h");
    expect(elapsedShort(now - 48 * 3_600_000, now)).toBe("2d");
    expect(elapsedShort(now + 5_000, now)).toBe("0s");
    expect(elapsedShort(undefined, now)).toBe("");
  });
});

test("a prompt's title skips the agent's tip and boilerplate lines", () => {
  const request = "Tip: auto mode handles this for you\ncurl -sI https://example.edu\nThis command requires approval\nDo you want to proceed?";
  expect(promptTitle({ id: "p", kind: "confirm", request } as never)).toBe("curl -sI https://example.edu");
});

test("a wrapped Tip is skipped whole in a prompt's title", () => {
  const request = 'Tip: auto mode handles these prompts for you — choose "switch to auto\nmode" below\nFetch HTTP headers from example.edu\ncurl -sI https://example.edu';
  expect(promptTitle({ id: "p", kind: "permission", request } as never)).toBe("Fetch HTTP headers from example.edu");
});

describe("the Working line (SPEC §11)", () => {
  const prompt = { id: "p", kind: "permission", provider: "claude", title: "Bash command", options: [] } as never;
  test("shows while the agent works, not while a request is open, nor once it is blocked or ended", () => {
    expect(workingLineShown({ status: "running" }, true)).toBe(true);
    expect(workingLineShown({ status: "running", prompt }, true)).toBe(false);
    expect(workingLineShown({ status: "running", approvals: [{ id: "a", tool: "Bash", options: [] }] }, true)).toBe(false);
    expect(workingLineShown({ status: "blocked" }, true)).toBe(false);
    expect(workingLineShown({ status: "done" }, true)).toBe(false);
    expect(workingLineShown({ status: "running" }, false)).toBe(false);
  });
  test("counts from the daemon's running_since when it has one", () => {
    expect(workingSince({ running_since: Date.parse("2026-11-12T20:47:07Z") }, "2026-11-12T18:00:00Z")).toBe("2026-11-12T20:47:07.000Z");
    expect(workingSince({}, "2026-11-12T18:00:00Z")).toBe("2026-11-12T18:00:00Z");
  });
});
