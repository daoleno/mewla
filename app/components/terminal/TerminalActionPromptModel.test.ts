import { describe, expect, test } from "bun:test";
import { buildTerminalActionPrompt } from "./TerminalActionPromptModel";
import type { WorkerPrompt } from "../../store/workers";
import {
  bumpServerConnectionGeneration,
  isWorkerSessionListFreshForConnection,
  liveActionPromptScopeKey,
  stampWorkerSessionListGeneration,
  type TransportConnectionState,
} from "../../services/workerSessionListTransport";

const permission: WorkerPrompt = {
  id: "perm-1",
  kind: "permission",
  provider: "claude",
  title: "Bash command",
  request: "touch /tmp/x\nAPI_TOKEN=abc123 curl localhost",
  options: [
    { number: 1, label: "Yes", selected: true },
    { number: 2, label: "Yes, and don't ask again for touch commands", description: "Applies to this project" },
    { number: 3, label: "No" },
  ],
};

const plan: WorkerPrompt = {
  id: "plan-1",
  kind: "plan",
  provider: "claude",
  title: "Waiting for plan approval",
  request: "1. Add the card\n2. Test it",
  options: [
    { number: 1, label: "Yes, and use auto mode", selected: true },
    { number: 2, label: "Yes, manually approve edits" },
    { number: 3, label: "Tell Claude what to change" },
  ],
};

const startup: WorkerPrompt = {
  id: "startup-1",
  kind: "startup",
  provider: "claude",
  title: "Do you trust the files in this folder?",
  options: [
    { number: 1, label: "Yes, I trust this folder", selected: true },
    { number: 2, label: "No, exit" },
  ],
};

const codexApproval: WorkerPrompt = {
  id: "codex-1",
  kind: "approval",
  provider: "codex",
  title: "Would you like to run the following command?",
  request: "$ rm -rf build",
  options: [
    { number: 1, label: "Yes, proceed", selected: true },
    { number: 2, label: "No, and tell Codex what to do differently" },
  ],
};

const choice: WorkerPrompt = {
  id: "choice-1",
  kind: "choice",
  provider: "claude",
  title: "Question",
  questions: [{ question: "Which?", options: [{ number: 1, label: "A" }] }],
};

type TransportSlice = {
  connectionState: TransportConnectionState;
  connectionGenerationByServer: Record<string, number>;
  workerSessionListGenerationByServer: Record<string, number>;
};

function applyConnection(
  slice: TransportSlice,
  serverId: string,
  connectionState: TransportConnectionState,
): TransportSlice {
  return {
    connectionState,
    connectionGenerationByServer: bumpServerConnectionGeneration(
      slice.connectionGenerationByServer,
      serverId,
      slice.connectionState,
      connectionState,
    ),
    workerSessionListGenerationByServer: slice.workerSessionListGenerationByServer,
  };
}

function applyFullWorkerSessionList(slice: TransportSlice, serverId: string): TransportSlice {
  return {
    ...slice,
    workerSessionListGenerationByServer: stampWorkerSessionListGeneration({
      connectionState: slice.connectionState,
      connectionGeneration: slice.connectionGenerationByServer[serverId] ?? 0,
      workerSessionListGenerationByServer: slice.workerSessionListGenerationByServer,
      serverId,
    }),
  };
}

function isFresh(slice: TransportSlice, serverId: string) {
  return isWorkerSessionListFreshForConnection({
    connectionState: slice.connectionState,
    connectionGeneration: slice.connectionGenerationByServer[serverId] ?? 0,
    workerSessionListGeneration:
      slice.workerSessionListGenerationByServer[serverId] ?? 0,
  });
}

function projectPrompt(
  slice: TransportSlice,
  serverId: string,
  agent: { id: string; prompt?: WorkerPrompt; processId?: number; startedAt?: number },
) {
  if (!isFresh(slice, serverId)) {
    return null;
  }
  return buildTerminalActionPrompt({
    prompt: agent.prompt,
    scopeKey: liveActionPromptScopeKey({
      workerId: agent.id,
      processId: agent.processId,
      startedAt: agent.startedAt,
      connectionGeneration: slice.connectionGenerationByServer[serverId] ?? 0,
    }),
  });
}

const LIVE_BLOCKED = { id: "claude-1", prompt: plan, processId: 41413, startedAt: 900 };

describe("buildTerminalActionPrompt from the daemon's prompt", () => {
  test("a permission card answers by option number and redacts the request", () => {
    const card = buildTerminalActionPrompt({ prompt: permission });
    expect(card?.title).toBe("Bash command");
    expect(card?.promptId).toBe("perm-1");
    expect(card?.requestText).toBe("touch /tmp/x\nAPI_TOKEN=[redacted] curl localhost");
    expect(card?.options.map((o) => [o.id, o.number, o.label])).toEqual([
      ["1", 1, "Yes"],
      ["2", 2, "Yes, and don't ask again for touch commands"],
      ["3", 3, "No"],
    ]);
    expect(card?.options[1].description).toBe("Applies to this project");
    expect(card?.options[0]).toMatchObject({ primary: true, default: true });
    expect(card?.options[2].destructive).toBe(true);
    expect(card?.options[0].destructive).toBe(false);
    expect(card?.defaultOptionId).toBe("1");
    expect(card?.approvalId).toBeUndefined();
  });

  test("a plan card shows the plan and Claude's own labels", () => {
    const card = buildTerminalActionPrompt({ prompt: plan });
    expect(card?.title).toBe("Waiting for plan approval");
    expect(card?.requestText).toBe("1. Add the card\n2. Test it");
    expect(card?.options.map((o) => o.label)).toEqual([
      "Yes, and use auto mode",
      "Yes, manually approve edits",
      "Tell Claude what to change",
    ]);
    expect(card?.options.map((o) => o.number)).toEqual([1, 2, 3]);
  });

  test("a startup card and a Codex approval card are menu cards too", () => {
    const folder = buildTerminalActionPrompt({ prompt: startup });
    expect(folder?.title).toBe("Do you trust the files in this folder?");
    expect(folder?.requestText).toBeUndefined();
    expect(folder?.options.map((o) => o.number)).toEqual([1, 2]);
    const codex = buildTerminalActionPrompt({ prompt: codexApproval });
    expect(codex?.requestText).toBe("$ rm -rf build");
    expect(codex?.options[1].destructive).toBe(true);
  });

  test("no card carries a key press or instructions to use the terminal", () => {
    for (const prompt of [permission, plan, startup, codexApproval]) {
      const card = buildTerminalActionPrompt({ prompt });
      const json = JSON.stringify(card);
      expect(json).not.toMatch(/terminal/i);
      expect(card?.options.every((o) => !("key" in o))).toBe(true);
      expect(card).not.toHaveProperty("detail");
      expect(card).not.toHaveProperty("actionable");
    }
  });

  test("a choice prompt is the choice card's, not a menu", () => {
    expect(buildTerminalActionPrompt({ prompt: choice })).toBeNull();
  });

  test("a Session blocked without a structured prompt has no card", () => {
    expect(buildTerminalActionPrompt({})).toBeNull();
    expect(buildTerminalActionPrompt({ prompt: undefined, approvals: [] })).toBeNull();
    expect(buildTerminalActionPrompt({ prompt: { ...plan, options: [] } })).toBeNull();
  });

  test("scopes the card id to the live process/session generation", () => {
    const first = buildTerminalActionPrompt({
      prompt: plan,
      scopeKey: liveActionPromptScopeKey({ workerId: "agent-a", processId: 11, startedAt: 100, connectionGeneration: 1 }),
    });
    const second = buildTerminalActionPrompt({
      prompt: plan,
      scopeKey: liveActionPromptScopeKey({ workerId: "agent-a", processId: 22, startedAt: 200, connectionGeneration: 2 }),
    });
    expect(first?.id).toBe("agent-a:11:100:1|plan-1");
    expect(second?.id.startsWith("agent-a:22:200:2|")).toBe(true);
    expect(first?.id).not.toBe(second?.id);
    expect(first?.promptId).toBe("plan-1");
  });
});

describe("worker_session_list connection-generation freshness", () => {
  test("connect -> full snapshot allows current live prompt", () => {
    let slice: TransportSlice = {
      connectionState: "offline",
      connectionGenerationByServer: {},
      workerSessionListGenerationByServer: {},
    };
    slice = applyConnection(slice, "server", "connecting");
    slice = applyConnection(slice, "server", "connected");
    expect(isFresh(slice, "server")).toBe(false);

    slice = applyFullWorkerSessionList(slice, "server");
    expect(isFresh(slice, "server")).toBe(true);
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)?.title).toBe("Waiting for plan approval");
  });

  test("disconnect hides; reconnect with retained identical data stays hidden until new full list", () => {
    let slice: TransportSlice = {
      connectionState: "offline",
      connectionGenerationByServer: {},
      workerSessionListGenerationByServer: {},
    };
    slice = applyConnection(slice, "server", "connecting");
    slice = applyConnection(slice, "server", "connected");
    slice = applyFullWorkerSessionList(slice, "server");
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)?.promptId).toBe("plan-1");

    slice = applyConnection(slice, "server", "connecting");
    expect(isFresh(slice, "server")).toBe(false);
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)).toBeNull();

    // Reconnect while retained agent rows still carry the identical menu.
    slice = applyConnection(slice, "server", "connected");
    expect(isFresh(slice, "server")).toBe(false);
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)).toBeNull();

    // Incremental agent upsert is not a full worker_session_list.
    expect(isFresh(slice, "server")).toBe(false);

    slice = applyFullWorkerSessionList(slice, "server");
    expect(isFresh(slice, "server")).toBe(true);
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)?.title).toBe("Waiting for plan approval");
  });

  test("fresh resolved snapshot stays absent after full list", () => {
    let slice: TransportSlice = {
      connectionState: "offline",
      connectionGenerationByServer: {},
      workerSessionListGenerationByServer: {},
    };
    slice = applyConnection(slice, "server", "connected");
    slice = applyFullWorkerSessionList(slice, "server");
    expect(isFresh(slice, "server")).toBe(true);
    expect(projectPrompt(slice, "server", { id: "claude-1" })).toBeNull();
  });
});

describe("buildTerminalActionPrompt with a decision the agent reported", () => {
  const approval = {
    id: "a1",
    kind: "command",
    tool: "shell",
    summary: "touch /tmp/x",
    options: [
      { id: "accept", label: "Allow", scope: "once" },
      { id: "cancel", label: "Deny and stop the turn", scope: "deny" },
    ],
  };

  test("answers through the agent ahead of a terminal prompt", () => {
    const prompt = buildTerminalActionPrompt({
      prompt: plan,
      approvals: [approval],
      scopeKey: "w|1",
    });
    expect(prompt?.id).toBe("w|1|approval:a1");
    expect(prompt?.approvalId).toBe("a1");
    expect(prompt?.requestText).toBe("touch /tmp/x");
    expect(prompt?.options.map((o) => [o.id, o.approvalOptionId, o.number])).toEqual([
      ["accept", "accept", undefined],
      ["cancel", "cancel", undefined],
    ]);
    expect(prompt?.promptId).toBeUndefined();
    expect(prompt?.options[0]).toMatchObject({ primary: true, default: true });
    expect(prompt?.options[1].destructive).toBe(true);
    expect(prompt?.defaultOptionId).toBe("accept");
  });

  test("a question approval is not a decision card", () => {
    expect(buildTerminalActionPrompt({ approvals: [{ ...approval, tool: "AskUserQuestion" }] })).toBeNull();
  });

  test("a question approval does not hide a real permission behind it", () => {
    const prompt = buildTerminalActionPrompt({
      approvals: [{ ...approval, id: "q1", tool: "AskUserQuestion" }, approval],
    });
    expect(prompt?.approvalId).toBe("a1");
  });

  test("no prompt and no decision means no card", () => {
    expect(buildTerminalActionPrompt({ approvals: undefined })).toBeNull();
  });
});
