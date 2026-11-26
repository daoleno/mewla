import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { CodexConversationEvent, ConversationTool } from "../conversation/conversation";
import { commandTitle, toolStepActivity, toolStepTitle } from "./toolStep";
import { createToolRunFolder } from "./foldToolRuns";
import { buildTimeline } from "./model";
import type { ActivityTimelineItem } from "./activityTypes";
import { toolPictures } from "./threadPictures";

// The daemon's reading of recorded real turns of every agent (daemon/work/testdata/tooldisplay, owner 11-27).
const GOLDEN = join(import.meta.dir, "../../../daemon/work/testdata/tooldisplay");
const AGENTS = ["claude", "brain", "codex", "cursor", "grok", "opencode", "pi", "antigravity"];
const recorded = (agent: string): { call: string; tool: ConversationTool }[] =>
  JSON.parse(readFileSync(join(GOLDEN, `${agent}.golden.json`), "utf8"));

const BARE = /^(Run|Read|Search|Edit|Use|List|Open|Fetch|View|Test|Build)$/;

describe("a step's title (owner 11-27 note 1: never a bare verb)", () => {
  for (const agent of AGENTS) {
    test(`${agent}: every recorded call has a title that says what it did to what`, () => {
      const tools = recorded(agent);
      expect(tools.length).toBeGreaterThan(4);
      for (const { tool } of tools) {
        const title = toolStepTitle(tool);
        expect(title.trim()).not.toMatch(BARE);
        expect(title.length).toBeGreaterThan(3);
      }
    });
  }

  test("a command's title says what it is for when its program says so", () => {
    expect(commandTitle("gh run view 45768803895 --repo me/mewla --log")).toBe("Check run 45768803895");
    expect(commandTitle("timeout 580 gh run watch 45768941715 --repo me/x --interval 30")).toBe("Watch run 45768941715");
    expect(commandTitle("go test ./watcher/ -count=1")).toBe("Test ./watcher/");
    expect(commandTitle("git log --oneline -8")).toBe("Git log");
    expect(commandTitle("sed -n '160,230p' tests/site.test.ts")).toBe("Read tests/site.test.ts");
    expect(commandTitle("ssh -o BatchMode=yes me@mac 'ls'")).toBe("Run on mac");
    expect(commandTitle("tmux -S /tmp/s capture-pane -t main:0.0 -p")).toBe("tmux capture-pane");
    expect(commandTitle('"$MEWLA_WORKER_PROGRESS_CMD" worker progress --status running')).toBe("Report progress");
    expect(commandTitle("adb shell input tap 320 1950")).toBe("Run adb");
    expect(commandTitle("mkdir -p /tmp/flight/after")).toBe("Create folder after");
    expect(commandTitle("for f in before-phone.png after.png; do echo $f; done")).toBe("Run a shell loop");
  });

  test("the agent's own words lead when it gave some", () => {
    expect(toolStepTitle({ kind: "command", target: "timeout 40 ssh mac 'screencapture -x'", intent: "Test screencapture over SSH", status: "failed" })).toBe("Test screencapture over SSH");
  });
});

const event = (id: string, tool: ConversationTool, extra: Partial<CodexConversationEvent> = {}): CodexConversationEvent => ({
  id, seq: Number(id.replace(/\D/g, "")) || 1, kind: tool.kind === "command" ? "command" : "tool", tool_name: tool.kind, status: tool.status, tool, ...extra,
});

describe("a turn's calls as a Ledger", () => {
  test("a Sleep is no row (note 3), a failure opens itself with the end of its output (note 2)", () => {
    const sleep = toolStepActivity(event("e1", { kind: "wait", status: "done", duration_ms: 60_000 }));
    expect(sleep).toBeNull();
    const failed = toolStepActivity(event("e2", { kind: "command", target: "go test ./x", status: "failed", result: "exit 1", duration_ms: 515 },
      { command: "go test ./x", output: Array.from({ length: 20 }, (_, index) => `line ${index + 1}`).join("\n"), exit_code: 1 }))!;
    expect(failed.defaultExpanded).toBe(true);
    expect(failed.tone).toBe("failed");
    expect(failed.body?.endsWith("line 20")).toBe(true);
    expect(failed.body?.includes("line 1\n")).toBe(false);
    expect(failed.ledger).toMatchObject({ result: "exit 1", durationMs: 515 });
  });

  test("a run keeps its failures and its reasoning, and says its time and how many failed", () => {
    const events: CodexConversationEvent[] = [
      event("e1", { kind: "read", target: "/w/daemon/work/a.go", status: "done", duration_ms: 40 }),
      { id: "r1", seq: 2, kind: "commentary", body: "The gate needs a Worker id.", status: "done" },
      event("e3", { kind: "command", target: "go test ./work", status: "failed", result: "exit 1", duration_ms: 2_000 }, { command: "go test ./work", output: "FAIL", exit_code: 1 }),
      event("e4", { kind: "search", target: "needs", scope: "cmd", status: "done", result: "2 results", duration_ms: 60 }),
      event("e5", { kind: "wait", status: "done", duration_ms: 60_000 }),
    ];
    const items = createToolRunFolder()(buildTimeline(events)) as ActivityTimelineItem[];
    expect(items).toHaveLength(1);
    const run = items[0];
    expect(run.title).toBe("Ran 1 command, read 1 file, searched once");
    expect(run.ledger).toMatchObject({ durationMs: 2_100, failed: 1 });
    expect(run.steps?.map((step) => step.activityKind ?? step.title)).toEqual(["Read daemon/work/a.go", "reasoning", "Test ./work", "Search “needs” in cmd"]);
  });

  test("a picture a call looked at still shows under its run", () => {
    const events: CodexConversationEvent[] = [
      event("e1", { kind: "command", target: "ls docs", status: "done", duration_ms: 30 }, { command: "ls docs" }),
      event("e2", { kind: "image", target: "/tmp/fixture-preview.png", status: "done", duration_ms: 12 },
        { tool_name: "exec", input: 'text(await tools.view_image({path:"/tmp/fixture-preview.png"}))' }),
      event("e3", { kind: "read", target: "/tmp/screen.png", status: "done" }, { tool_name: "Read", input: '{"file_path":"/tmp/screen.png"}' }),
    ];
    const items = createToolRunFolder()(buildTimeline(events)) as ActivityTimelineItem[];
    expect(items.flatMap(toolPictures)).toEqual(["/tmp/fixture-preview.png", "/tmp/screen.png"]);
  });
});
