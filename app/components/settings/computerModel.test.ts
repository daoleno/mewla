import { describe, expect, test } from "bun:test";
import type { AgentUseStatus } from "../../services/agentUse";
import { computerProblems, computerProblemsMayChange, computerStatusWord } from "./computerModel";

const macMissingExtension: AgentUseStatus = {
  enabled: true,
  browser: {
    state: "no_extension",
    summary: "Add the Playwright extension to Chrome so agents can use your logged-in sites.",
    fix: "install_extension",
  },
  computer: { state: "ready", summary: "Agents can use the screen." },
};

const headlessLinux: AgentUseStatus = {
  enabled: true,
  browser: { state: "no_desktop", summary: "Sign in to this computer's desktop so agents can open Chrome there." },
  computer: { state: "unsupported", summary: "Agents can use the screen on Macs for now." },
};

describe("computerStatusWord", () => {
  test("a computer not in use is online when it answers, not offline by default", () => {
    expect(computerStatusWord({ current: false, connection: undefined, issue: null, check: { state: "online" } })).toBe("Online");
    expect(computerStatusWord({ current: false, connection: undefined, issue: null, check: undefined })).toBe("");
    expect(computerStatusWord({ current: false, connection: undefined, issue: null, check: { state: "offline" } })).toBe("Offline");
    expect(computerStatusWord({ current: false, connection: undefined, issue: null, check: { state: "unpaired" } })).toBe("Pair again");
  });

  test("the computer in use says so, or what is wrong with it", () => {
    expect(computerStatusWord({ current: true, connection: "connected", issue: null, check: undefined })).toBe("In use");
    expect(computerStatusWord({ current: true, connection: "connecting", issue: null, check: undefined })).toBe("Connecting…");
    expect(computerStatusWord({ current: true, connection: "offline", issue: "network_unreachable", check: undefined })).toBe("Can't connect");
    expect(computerStatusWord({ current: true, connection: "offline", issue: "device_not_paired", check: undefined })).toBe("Pair again");
    // Disconnected on this phone: answering a probe doesn't make it in use.
    expect(computerStatusWord({ current: true, connection: "offline", issue: null, check: { state: "online" } })).toBe("Not connected");
  });
});

describe("computerProblems", () => {
  test("a Mac without the extension shows one line and one button", () => {
    const problems = computerProblems(macMissingExtension);
    expect(problems).toHaveLength(1);
    expect(problems[0].text).toBe("Chrome needs the Playwright extension.");
    expect(problems[0].action?.label).toBe("Add extension");
    expect(problems[0].action?.does).toEqual({ kind: "fix", fix: "install_extension", target: "browser" });
    expect(computerProblemsMayChange(macMissingExtension)).toBe(true);
  });

  test("a headless Linux server shows nothing and is not polled", () => {
    expect(computerProblems(headlessLinux)).toEqual([]);
    expect(computerProblemsMayChange(headlessLinux)).toBe(false);
  });

  test("a computer agents can't use shows nothing to fix, except an agent still on the screen", () => {
    const off: AgentUseStatus = { ...macMissingExtension, enabled: false };
    expect(computerProblems(off)).toEqual([]);
    const busy: AgentUseStatus = {
      ...off,
      computer: { state: "ready", summary: "", active: { worker: "w1", name: "Codex", since: "" } },
    };
    expect(computerProblems(busy).map((problem) => [problem.text, problem.action?.label])).toEqual([["Codex is using the screen.", "Stop"]]);
  });

  test("a missing screen permission opens Settings on the Mac", () => {
    const problems = computerProblems({
      enabled: true,
      browser: { state: "ready", summary: "" },
      computer: { state: "permission", summary: "", fix: "open_settings", permission: "Screen Recording" },
    });
    expect(problems.map((problem) => [problem.text, problem.action?.label])).toEqual([
      ["Allow Mewla Screen Recording access on this Mac.", "Open Settings"],
    ]);
  });

  test("a missing extension key is opened in Chrome, then pasted", () => {
    const problems = computerProblems({ enabled: true, browser: { state: "no_key", summary: "", fix: "open_extension" } });
    expect(problems.map((problem) => problem.action?.does)).toEqual([
      { kind: "fix", fix: "open_extension", target: "browser" },
      { kind: "paste_key" },
    ]);
    const headless = computerProblems({ enabled: true, browser: { state: "no_key", summary: "" } });
    expect(headless.map((problem) => problem.action?.does)).toEqual([{ kind: "paste_key" }]);
  });
});
