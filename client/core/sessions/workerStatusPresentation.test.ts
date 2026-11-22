import { describe, expect, test } from "bun:test";
import { sessionStateWord } from "./workerStatusPresentation";

describe("the Session header's state word", () => {
  test("its agent's restart while that is the news, else its status", () => {
    expect(sessionStateWord({ status: "running", relaunch: { state: "restarting", line: "Restarting after a crash" } })).toBe("Restarting");
    expect(sessionStateWord({ status: "done", relaunch: { state: "restarted", line: "Restarted after a crash" } })).toBe("Restarted");
    expect(sessionStateWord({ status: "failed", relaunch: { state: "stopped", line: "Stopped after 3 restarts" } })).toBe("Stopped");
    expect(sessionStateWord({ status: "blocked", relaunch: { state: "waiting", line: "Waiting for Claude Code's provider" } })).toBe("Waiting");
    expect(sessionStateWord({ status: "failed" })).toBe("Failed");
  });
});
