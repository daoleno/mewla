import { describe, expect, test } from "bun:test";
import { sessionStateWord } from "./workerStatusPresentation";

describe("the Session header's state word", () => {
  test("its agent's restart while that is the news, else its status", () => {
    expect(sessionStateWord({ status: "running", relaunch: { state: "restarting", line: "Restarting after SIGKILL" } })).toBe("Restarting");
    expect(sessionStateWord({ status: "done", relaunch: { state: "restarted", line: "Restarted after SIGKILL" } })).toBe("Restarted");
    expect(sessionStateWord({ status: "failed", relaunch: { state: "not_restarted", line: "Gave up after 3 restarts" } })).toBe("Not restarted");
    expect(sessionStateWord({ status: "failed" })).toBe("Failed");
  });
});
