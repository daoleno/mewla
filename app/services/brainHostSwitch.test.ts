import { expect, test } from "bun:test";
import { brainHostSwitchProgress } from "./brainHostSwitch";

const executor = (id: string, provider: string) => ({ id, name: id, provider });
const worker = (id: string, provider?: string) => ({
  id,
  name: "Brain",
  status: "unknown",
  provider,
});

test("a switch is pending while the old host is still the host", () => {
  expect(
    brainHostSwitchProgress(
      {
        host_executor: executor("claude", "claude"),
        host_worker: worker("%2", "codex"),
      },
      "claude",
    ),
  ).toEqual({ state: "pending" });
});

test("a switch is pending while the new host is not yet observed", () => {
  expect(
    brainHostSwitchProgress(
      {
        host_executor: executor("claude", "claude"),
        host_worker: worker("%3"),
      },
      "claude",
    ),
  ).toEqual({ state: "pending" });
});

test("a snapshot that still names the previous executor is pending", () => {
  expect(
    brainHostSwitchProgress(
      {
        host_executor: executor("codex", "codex"),
        host_worker: worker("%2", "codex"),
      },
      "claude",
    ),
  ).toEqual({ state: "pending" });
});

test("the new host showing up in the snapshot completes the switch", () => {
  expect(
    brainHostSwitchProgress(
      {
        host_executor: executor("claude", "claude"),
        host_worker: worker("%3", "claude"),
      },
      "claude",
    ),
  ).toEqual({ state: "ready" });
});

test("a host problem for the chosen executor is the failure, with its reason", () => {
  expect(
    brainHostSwitchProgress(
      {
        host_executor: executor("claude", "claude"),
        host_worker: worker("%2", "codex"),
        host_problem: {
          code: "start_failed",
          executor: "claude",
          detail: "Brain could not start on claude: tmux refused the session",
          installed_executors: [],
        },
      },
      "claude",
    ),
  ).toEqual({
    state: "failed",
    detail: "Brain could not start on claude: tmux refused the session",
  });
});

test("a host problem for another executor is not this switch's failure", () => {
  expect(
    brainHostSwitchProgress(
      {
        host_executor: executor("claude", "claude"),
        host_worker: worker("%2", "codex"),
        host_problem: {
          code: "not_signed_in",
          executor: "codex",
          detail: "codex is not signed in",
          installed_executors: [],
        },
      },
      "claude",
    ),
  ).toEqual({ state: "pending" });
});
