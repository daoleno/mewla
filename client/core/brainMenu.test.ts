import { describe, expect, test } from "bun:test";
import type { BrainSnapshot } from "./brain";
import { brainExecutorChoices, brainExecutorMeta, brainHostSwitchProgress, brainMenuRows, canSwitchBrainExecutor, canSwitchBrainExecutorFromProblem } from "./brainMenu";

const claude = { id: "claude", name: "", provider: "claude" };
const codex = { id: "codex", name: "", provider: "codex", runtime: "gpt-5" };
const grok = { id: "grok", name: "", provider: "grok" };
const snapshot: BrainSnapshot = {
  host_worker: { id: "brain-host", name: "Brain", status: "running", provider: "claude" },
  host_executor: claude,
  chat_thread_id: "t1",
  executors: [claude, codex, grok],
  executor_states: { claude: { installed: true, signed_in: true }, codex: { installed: true, signed_in: false, account: "me@x" }, grok: { installed: false, signed_in: false } },
};

describe("Brain's menu (soft-final/SPEC.md §1.6)", () => {
  const enabled = (input: Parameters<typeof brainMenuRows>[0]) => brainMenuRows(input).filter((row) => row.enabled).map((row) => row.id);
  const running = { connected: true, snapshot, threadLoaded: true, modelSwitchable: true };

  test("always five rows in their order: Open terminal, Browse workspace, Switch executor, Model, New chat", () => {
    expect(brainMenuRows(running).map((row) => row.label)).toEqual(["Open terminal", "Browse workspace", "Switch executor", "Model", "New chat"]);
    expect(brainMenuRows({ ...running, connected: false, snapshot: null })).toHaveLength(5);
    expect(enabled(running)).toEqual(["terminal", "workspace", "executor", "model", "new-chat"]);
  });

  test("a row the computer cannot do now stays, not enabled", () => {
    // Offline: only Brain's terminal (its offline card).
    expect(enabled({ ...running, connected: false })).toEqual(["terminal"]);
    // Can't start: the workspace and the switch, no terminal, model or new chat.
    const cantStart = { ...snapshot, host_worker: null, host_problem: { code: "not_signed_in", executor: "codex", detail: "Sign in.", installed_executors: ["claude"] } };
    expect(enabled({ ...running, snapshot: cantStart })).toEqual(["workspace", "executor"]);
    // New chat waits for the thread; Model for a route that can switch.
    expect(enabled({ ...running, threadLoaded: false, modelSwitchable: false })).toEqual(["terminal", "workspace", "executor"]);
    // Only one installed executor: nothing to switch to.
    const alone = { ...snapshot, executor_states: { claude: { installed: true, signed_in: true } } };
    expect(enabled({ ...running, snapshot: alone })).toEqual(["terminal", "workspace", "model", "new-chat"]);
  });
});

describe("Switch executor", () => {
  test("offers installed executors, signed-in first, and says when more can be installed", () => {
    const { choices, moreToInstall } = brainExecutorChoices(snapshot.executors!, snapshot.executor_states);
    expect(choices.map((e) => e.id)).toEqual(["claude", "codex"]);
    expect(moreToInstall).toBe(true);
    expect(canSwitchBrainExecutor(snapshot)).toBe(true);
    expect(brainExecutorMeta(codex, snapshot.executor_states!.codex)).toBe("Codex · gpt-5 · me@x");
  });

  test("a switch is done when the new host shows up, failed on its host problem", () => {
    expect(brainHostSwitchProgress(snapshot, "codex")).toEqual({ state: "pending" });
    const failed = { ...snapshot, host_problem: { code: "not_signed_in", executor: "codex", detail: "Codex is not signed in.", installed_executors: [] } };
    expect(brainHostSwitchProgress(failed, "codex")).toEqual({ state: "failed", detail: "Codex is not signed in." });
    const ready = { ...snapshot, host_executor: codex, host_worker: { ...snapshot.host_worker!, provider: "codex" } };
    expect(brainHostSwitchProgress(ready, "codex")).toEqual({ state: "ready" });
  });
});

describe("Brain can't start", () => {
  test("Switch executor is a way out only with another agent installed", () => {
    expect(canSwitchBrainExecutorFromProblem({ installed_executors: ["codex"] })).toBe(true);
    expect(canSwitchBrainExecutorFromProblem({ installed_executors: [] })).toBe(false);
  });
});
