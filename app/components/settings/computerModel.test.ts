import { describe, expect, test } from "bun:test";
import type { AgentUseStatus } from "../../services/agentUse";
import { computerProblems, computerProblemsMayChange, computerStatusWord, computerUpdateProblem } from "./computerModel";

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
  test("a Mac without the extension shows one neutral row that starts setup, not a problem", () => {
    const problems = computerProblems(macMissingExtension);
    expect(problems).toHaveLength(1);
    expect(problems[0].text).toBe("Let agents use Chrome");
    expect(problems[0].text).not.toMatch(/needs|Playwright/);
    expect(problems[0].action?.label).toBe("Set up");
    expect(problems[0].action?.does).toEqual({ kind: "fix", fix: "install_extension", target: "browser" });
    expect(computerProblemsMayChange(macMissingExtension)).toBe(true);
  });

  test("a missing Chrome or Node.js is not shown to someone who never started setup", () => {
    for (const state of ["no_chrome", "no_node"] as const) {
      expect(computerProblems({ ...macMissingExtension, browser: { state, summary: "" } })).toEqual([]);
    }
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
    const mewla = computerProblems({ enabled: true, browser: { state: "no_key", summary: "", fix: "install_extension" } });
    expect(mewla.map((problem) => problem.action?.does)).toEqual([
      { kind: "fix", fix: "install_extension", target: "browser" },
      { kind: "paste_key" },
    ]);
    const headless = computerProblems({ enabled: true, browser: { state: "no_key", summary: "" } });
    expect(headless.map((problem) => problem.action?.does)).toEqual([{ kind: "paste_key" }]);
  });
});

describe("computerUpdateProblem", () => {
  const min = "0.2.88";
  test("a computer too old to report a version says nothing", () => {
    expect(computerUpdateProblem(null, min)).toBeNull();
  });

  test("a newer release is one line and one Update button", () => {
    expect(computerUpdateProblem({ version: "0.2.88", supported: true, latest: "0.2.89" }, min)).toEqual({
      key: "update",
      text: "Update available · 0.2.89",
      action: { label: "Update", does: { kind: "update" } },
    });
  });

  test("an up to date computer shows nothing", () => {
    expect(computerUpdateProblem({ version: "0.2.89", supported: true }, min)).toBeNull();
  });

  test("a computer that does not update itself offers no button", () => {
    expect(computerUpdateProblem({ version: "0.2.88", supported: false, latest: "0.2.89" }, min)).toBeNull();
  });

  test("an app and computer that no longer fit say so with the same button", () => {
    expect(computerUpdateProblem({ version: "0.2.88", supported: true, latest: "0.2.90" }, "0.2.90")).toEqual({
      key: "update",
      text: "This computer needs an update to work with this app.",
      action: { label: "Update", does: { kind: "update" } },
    });
    // It cannot update itself: the line stays, the button does not.
    expect(computerUpdateProblem({ version: "0.2.88", supported: false }, "0.2.90")?.action).toBeUndefined();
  });

  test("while updating, no button; a failed update keeps its reason", () => {
    expect(computerUpdateProblem({ version: "0.2.88", supported: true, latest: "0.2.89", updating: true }, min)).toEqual({
      key: "update",
      text: "Updating…",
    });
    expect(
      computerUpdateProblem({ version: "0.2.88", supported: true, error: "Mewla 0.2.89 did not start; still on 0.2.88" }, min)?.text,
    ).toBe("Mewla 0.2.89 did not start; still on 0.2.88");
  });
});
