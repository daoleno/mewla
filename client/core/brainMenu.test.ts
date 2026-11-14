import { describe, expect, test } from "bun:test";
import type { BrainSnapshot } from "./brain";
import { brainExecutorChoices, brainExecutorLabel, brainExecutorMeta, brainHostSwitchProgress, brainMenuRows, canSwitchBrainExecutor, canSwitchBrainExecutorFromProblem } from "./brainMenu";

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

  test("its rows in their order: Open terminal, Browse workspace, Switch executor, Model, New chat", () => {
    expect(brainMenuRows(running).map((row) => row.label)).toEqual(["Open terminal", "Browse workspace", "Switch executor", "Model", "New chat"]);
    expect(enabled(running)).toEqual(["terminal", "workspace", "executor", "model", "new-chat"]);
  });

  test("a row the computer cannot do now is not drawn", () => {
    expect(brainMenuRows({ ...running, connected: false, snapshot: null })).toEqual([]);
    expect(brainMenuRows({ ...running, modelSwitchable: false }).map((row) => row.id)).not.toContain("model");
    expect(brainMenuRows({ ...running, modelSwitchable: false }).every((row) => row.enabled)).toBe(true);
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
  });

  test("one line per agent: its product name, no runtime; a second line only to tell rows apart or ask for a sign-in", () => {
    const states = snapshot.executor_states!;
    expect(brainExecutorLabel({ id: "codex", name: "codex", provider: "codex", runtime: "tmux" })).toBe("Codex");
    expect(brainExecutorLabel({ id: "agent", name: "agent", provider: "cursor" })).toBe("Cursor Agent");
    expect(brainExecutorLabel({ id: "oc", name: "", provider: "opencode" })).toBe("OpenCode");
    expect(brainExecutorLabel({ id: "work", name: "Work Codex", provider: "codex" })).toBe("Work Codex");
    expect(brainExecutorMeta(claude, states, [claude, codex])).toBe("");
    expect(brainExecutorMeta(codex, states, [claude, codex])).toBe("Not signed in");
    const codex2 = { id: "codex2", name: "codex2", provider: "codex" };
    const both = { ...states, codex: { installed: true, signed_in: true, account: "me@x" }, codex2: { installed: true, signed_in: true, account: "you@y" } };
    expect(brainExecutorMeta(codex, both, [codex, codex2])).toBe("me@x");
    expect(brainExecutorMeta(codex2, both, [codex, codex2])).toBe("you@y");
    expect(brainExecutorMeta(codex2, { ...both, codex2: { installed: true, signed_in: true } }, [codex, codex2])).toBe("codex2");
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
