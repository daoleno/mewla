import { describe, expect, test } from "bun:test";
import type { Worker } from "../sessions/workers";
import type { WorkWorkerSession } from "./types";
import { liveWorkerIds, workerName, workProject, workWorkerLines } from "./workers";

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
    expect(lines.map((l) => l.stateLabel)).toEqual([undefined, "Failed", undefined]);
  });

  test("a row says in words what is not plain running or done: Needs you, Failed, Ended", () => {
    const states = (["running", "blocked", "done", "failed", "ended"] as const).map((state) => workWorkerLines([{ ...rows[0]!, state }], new Map(), NOW)[0]!.stateLabel);
    expect(states).toEqual([undefined, "Needs you", undefined, "Failed", "Ended"]);
  });

  test("a live Session that settled reads its settled state; a Worker without a recorded name shows its executor, else Worker", () => {
    const sessions = new Map([["worker:@c", session("worker:@c", { status: "done" })]]);
    expect(workWorkerLines(rows, sessions, NOW)[2]).toMatchObject({ state: "done", progress: undefined });
    const nameless = workWorkerLines([{ ...rows[1]!, name: undefined }, { ...rows[1]!, name: undefined, executor_id: undefined, provider: undefined }], new Map(), NOW);
    expect(nameless.map((l) => [l.name, l.agent])).toEqual([["codex", "codex"], ["Worker", "terminal"]]);
  });

  test("a name never shows the Session id the daemon appends; another parenthesis stays", () => {
    expect(workerName("Hello writer (mh-17b3337d43f38037916f0e5b)")).toBe("Hello writer");
    expect(workerName("Fix (macOS)")).toBe("Fix (macOS)");
    const live = { ...rows[2]!, session_id: "mh-17b3337d43f38037916f0e5b", name: "Hello writer (mh-17b3337d43f38037916f0e5b)" };
    expect(workWorkerLines([live], new Map(), NOW)[0]!.name).toBe("Hello writer");
  });

  test("what a live Worker does is never its terminal's footer", () => {
    const footer = "⏵⏵ bypass permissions on (shift+tab to cycle)";
    const live = { ...rows[2]!, summary: footer, phase: undefined };
    // The list has its Session, which reported nothing yet: its state's word.
    const followed = workWorkerLines([live], new Map([["worker:@c", session("worker:@c", { summary: footer, last_output_lines: [footer] })]]), NOW)[0]!;
    expect(followed.summary).toBe("Working");
    // The list lacks it, and it reported no phase: nothing rather than the screen.
    expect(workWorkerLines([live], new Map(), NOW)[0]!.summary).toBe("");
    const manual = { ...live, summary: "⏸ manual mode on · ? for shortcuts" };
    expect(JSON.stringify(workWorkerLines([manual], new Map([["worker:@c", session("worker:@c", { summary: manual.summary })]]), NOW))).not.toContain("manual mode");
  });

  test("only live Workers fold into the Work's row", () => {
    expect(liveWorkerIds(rows)).toEqual(["worker:@c"]);
    expect(liveWorkerIds(undefined)).toEqual([]);
  });
});

describe("the project a Work is about", () => {
  test("its live Session's, else the first Worker the list still has, else none", () => {
    const sessions = new Map([["worker:@c", session("worker:@c", { cwd: "/home/me/atlas-docs/" })]]);
    const lines = workWorkerLines(rows, sessions, NOW);
    expect(lines.map((l) => l.project)).toEqual([undefined, undefined, "atlas-docs"]);
    expect(workProject("perpetuo", lines)).toBe("perpetuo");
    expect(workProject(undefined, lines)).toBe("atlas-docs");
    expect(workProject(undefined, workWorkerLines(rows, new Map(), NOW))).toBeUndefined();
  });
});
