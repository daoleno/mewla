import { describe, expect, test } from "bun:test";
import type { Worker } from "../sessions/workers";
import type { WorkWorkerSession } from "./types";
import { liveWorkerIds, workWorkerLines } from "./workers";

const NOW = new Date("2026-11-12T09:10:00Z");

function session(id: string, patch: Partial<Worker> = {}): Worker {
  return {
    key: `s1:${id}`, id, serverId: "s1", serverName: "Desk", serverUrl: "ws://x", name: id, status: "running",
    cwd: "/home/me/mewla", command: "claude", provider: "claude", summary: "", last_output_lines: [], updated_at: NOW.getTime(), ...patch,
  };
}

// WIRE §1's example: the first finished, the second failed, the third is live.
const rows: WorkWorkerSession[] = [
  { session_id: "worker:@a", name: "Planner", executor_id: "claude", provider: "claude", state: "done", summary: "Release plan written", live: false, joined_at: "2026-11-12T09:01:00Z", ended_at: "2026-11-12T09:03:00Z", updated_at: "2026-11-12T09:03:00Z" },
  { session_id: "worker:@b", name: "Builder", executor_id: "codex", provider: "codex", state: "failed", summary: "Build broke on Windows", live: false, joined_at: "2026-11-12T09:04:00Z", ended_at: "2026-11-12T09:06:00Z", updated_at: "2026-11-12T09:06:00Z" },
  { session_id: "worker:@c", name: "Fixer", executor_id: "claude", provider: "claude", state: "running", summary: "Rerunning the Windows build", phase: "verifying", live: true, joined_at: "2026-11-12T09:07:00Z", updated_at: "2026-11-12T09:08:00Z" },
];

describe("the Workers of a Work", () => {
  test("oldest first; finished rows read the snapshot, the live one its Session now", () => {
    const sessions = new Map([["worker:@c", session("worker:@c", { summary: "Windows build green, checking macOS", phase: "reporting" })]]);
    const lines = workWorkerLines(rows, sessions, NOW);
    expect(lines.map((l) => [l.name, l.agent, l.state, l.summary])).toEqual([
      ["Planner", "claude", "done", "Release plan written"],
      ["Builder", "codex", "failed", "Build broke on Windows"],
      ["Fixer", "claude", "running", "Windows build green, checking macOS"],
    ]);
    expect(lines[2]).toMatchObject({ progress: 5, time: "live", openable: true });
    expect(lines[0]!.progress).toBeUndefined();
    expect(lines[0]!.openable).toBe(false);
    expect(lines[0]!.time).not.toBe("live");
  });

  test("a live Session that settled reads its settled state; a Worker without a recorded name shows its executor, else Worker", () => {
    const sessions = new Map([["worker:@c", session("worker:@c", { status: "done" })]]);
    expect(workWorkerLines(rows, sessions, NOW)[2]).toMatchObject({ state: "done", progress: undefined });
    const nameless = workWorkerLines([{ ...rows[1]!, name: undefined }, { ...rows[1]!, name: undefined, executor_id: undefined, provider: undefined }], new Map(), NOW);
    expect(nameless.map((l) => [l.name, l.agent])).toEqual([["codex", "codex"], ["Worker", "terminal"]]);
  });

  test("only live Workers fold into the Work's row", () => {
    expect(liveWorkerIds(rows)).toEqual(["worker:@c"]);
    expect(liveWorkerIds(undefined)).toEqual([]);
  });
});
