/**
 * Brain's ⋯ menu and the Brain host switch, as the Expo app had them
 * (app/app/(primary)/index.tsx menuActions, components/brain/brainPresentation.ts,
 * services/brainHostSwitch.ts).
 */
import type { BrainExecutorRef, BrainExecutorState, BrainSnapshot } from "./brain";
import type { Transport } from "./transport";

export function brainProviderLabel(value?: string): string {
  switch (value?.trim().toLowerCase()) {
    case "codex":
      return "Codex";
    case "cursor":
      return "Cursor Agent";
    case "grok":
      return "Grok";
    case "claude":
      return "Claude Code";
    case "tmux":
      return "tmux";
    default:
      return value?.trim() || "Custom";
  }
}

/** An executor's name: its own, else its provider's. */
export function brainExecutorLabel(executor?: BrainExecutorRef | null): string {
  if (!executor) return "";
  if (executor.name?.trim()) return executor.name.trim();
  return brainProviderLabel(executor.provider && executor.provider !== "custom" ? executor.provider : executor.id);
}

/** The line under an executor's name: provider, runtime, the account it is signed in as. */
export function brainExecutorMeta(executor: BrainExecutorRef, state?: BrainExecutorState): string {
  return [brainProviderLabel(executor.provider), executor.runtime?.trim(), state?.account].filter(Boolean).join(" · ");
}

/**
 * The executors a person can choose: the ones installed on the computer,
 * signed-in ones first (otherwise in the computer's order). The rest are not
 * choices; `moreToInstall` says there are some, for one line pointing at
 * `mewla deps`.
 */
export function brainExecutorChoices(
  executors: readonly BrainExecutorRef[],
  states: Readonly<Record<string, BrainExecutorState>> | undefined,
): { choices: BrainExecutorRef[]; moreToInstall: boolean } {
  const installed = executors.filter((executor) => states?.[executor.id]?.installed);
  const signedIn = installed.filter((executor) => states?.[executor.id]?.signed_in);
  const others = installed.filter((executor) => !states?.[executor.id]?.signed_in);
  return { choices: [...signedIn, ...others], moreToInstall: installed.length < executors.length };
}

/** Switching needs an installed executor other than the one Brain runs on. */
export function canSwitchBrainExecutor(snapshot: BrainSnapshot | null | undefined): boolean {
  const { choices } = brainExecutorChoices(snapshot?.executors ?? [], snapshot?.executor_states);
  return choices.some((executor) => executor.id !== snapshot?.host_executor?.id);
}

/**
 * "Switch executor" on "Brain can't start" is a way out only when the computer
 * has another agent installed; with none, the problem's own sentence says what to install.
 */
export function canSwitchBrainExecutorFromProblem(problem: { installed_executors?: readonly string[] }): boolean {
  return (problem.installed_executors?.length ?? 0) > 0;
}

export type BrainMenuRowId = "terminal" | "workspace" | "executor" | "model" | "new-chat";
export type BrainMenuRow = { id: BrainMenuRowId; label: string; enabled: boolean };

export const BRAIN_MENU_LABELS: Record<BrainMenuRowId, string> = {
  terminal: "Open terminal",
  workspace: "Browse workspace",
  executor: "Switch executor",
  model: "Model",
  "new-chat": "New chat",
};

/** Brain runs: connected, with a host and no problem stated for it. */
export function brainRunning(input: { connected: boolean; snapshot: BrainSnapshot | null | undefined }): boolean {
  return input.connected && Boolean(input.snapshot?.host_worker?.id) && !input.snapshot?.host_problem;
}

/**
 * Brain's menu (soft-final/SPEC.md §1.6), always all five rows in their order; a
 * row the computer cannot do now is not enabled (drawn at 40 %, so the menu never
 * changes shape):
 * - Open terminal: Brain's host Session is known (also offline or connecting);
 * - Browse workspace: connected (Starting and Can't start too);
 * - Switch executor: connected, and another agent is installed;
 * - Model: Brain runs and its route can switch models;
 * - New chat: Brain runs and its thread has loaded.
 */
export function brainMenuRows(input: {
  connected: boolean;
  snapshot: BrainSnapshot | null | undefined;
  threadLoaded: boolean;
  modelSwitchable: boolean;
}): BrainMenuRow[] {
  const { connected, snapshot } = input;
  const running = brainRunning({ connected, snapshot });
  const enabled: Record<BrainMenuRowId, boolean> = {
    terminal: Boolean(snapshot?.host_worker?.id),
    workspace: connected,
    executor: connected && canSwitchBrainExecutor(snapshot),
    model: running && input.modelSwitchable,
    "new-chat": running && input.threadLoaded,
  };
  return (Object.keys(BRAIN_MENU_LABELS) as BrainMenuRowId[]).map((id) => ({ id, label: BRAIN_MENU_LABELS[id], enabled: enabled[id] }));
}

/**
 * Asks the computer to run Brain on another executor. It answers once the
 * choice is kept; the new host opens in its own time (`brainHostSwitchProgress`).
 * A refusal rejects with the computer's words (`brain_set_executor_failed`).
 */
export async function setBrainExecutor(transport: Pick<Transport, "request">, executorId: string): Promise<BrainSnapshot> {
  const frame = await transport.request<{ brain?: BrainSnapshot }>(
    "brain_set_executor",
    { executor_id: executorId, adapter_id: executorId },
    { reply: "brain_snapshot", timeoutMs: 15_000 },
  );
  return frame.brain ?? {};
}

/**
 * Where a Brain host switch stands, read from the snapshots the daemon already
 * pushes: "ready" once the new host shows up, "failed" on a host problem for
 * that executor, never a timer on the request.
 */
export type BrainHostSwitchProgress = { state: "pending" } | { state: "ready" } | { state: "failed"; detail: string };

/** How long the sheet shows a switch it has not seen finish; past it nothing is reported as failed. */
export const BRAIN_HOST_SWITCH_PATIENCE_MS = 90_000;

export function brainHostSwitchProgress(
  brain: Pick<BrainSnapshot, "host_executor" | "host_worker" | "host_problem"> | null | undefined,
  targetExecutorId: string,
): BrainHostSwitchProgress {
  const problem = brain?.host_problem;
  if (problem && problem.executor === targetExecutorId) return { state: "failed", detail: problem.detail };
  const executor = brain?.host_executor;
  const worker = brain?.host_worker;
  // The host worker names its provider only once the daemon observes the session.
  if (executor?.id === targetExecutorId && executor.provider && worker?.provider === executor.provider) return { state: "ready" };
  return { state: "pending" };
}
