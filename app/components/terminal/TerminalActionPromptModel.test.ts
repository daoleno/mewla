import { describe, expect, test } from "bun:test";
import { buildTerminalActionPrompt } from "./TerminalActionPromptModel";
import {
  bumpServerConnectionGeneration,
  isWorkerSessionListFreshForConnection,
  liveActionPromptScopeKey,
  stampWorkerSessionListGeneration,
  type TransportConnectionState,
} from "../../services/workerSessionListTransport";

const WEEKLY_LIMIT_LINES = [
  "◆ Task completed in 2m45s: Wait for dedicated em",
  "You hit your weekly limit.",
  "1 (O) Upgrade tier          Upgrade to a higher tier for more usage",
  "2 (O) Buy more credits      Purchase credits to keep using Grok Build",
  "↑/↓ navigate · y copy Enter:submit",
  "Esc:unselect | Tab:scrollback |",
];


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
  agent: {
    id: string;
    status: string;
    summary?: string;
    provider?: string;
    processId?: number;
    startedAt?: number;
    lastOutputLines?: string[];
  },
) {
  if (!isFresh(slice, serverId)) {
    return null;
  }
  return buildTerminalActionPrompt({
    status: agent.status,
    summary: agent.summary,
    lastOutputLines: agent.lastOutputLines,
    provider: agent.provider,
    scopeKey: liveActionPromptScopeKey({
      workerId: agent.id,
      processId: agent.processId,
      startedAt: agent.startedAt,
      connectionGeneration: slice.connectionGenerationByServer[serverId] ?? 0,
    }),
  });
}

const LIVE_BLOCKED = {
  id: "grok-1",
  status: "blocked",
  provider: "grok",
  summary: "↑/↓ navigate · y copy Enter:submit",
  processId: 41413,
  startedAt: 900,
  lastOutputLines: WEEKLY_LIMIT_LINES,
};

describe("buildTerminalActionPrompt", () => {
  const approval = (id: string, tool: string) => ({
    id,
    tool,
    options: [
      { id: "allow", label: "Allow", scope: "once" },
      { id: "deny", label: "Deny", scope: "deny" },
    ],
  });

  test("never turns an AskUserQuestion into an Allow/Deny card", () => {
    for (const tool of ["AskUserQuestion", "askuserquestion"]) {
      expect(
        buildTerminalActionPrompt({ status: "blocked", provider: "claude", approvals: [approval("a1", tool)] }),
      ).toBeNull();
    }
  });

  test("a question approval does not hide a real permission behind it", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      provider: "claude",
      approvals: [approval("a1", "AskUserQuestion"), approval("a2", "Bash")],
    });
    expect(prompt?.approvalId).toBe("a2");
    expect(prompt?.requestLabel).toBe("Bash");
  });

  test("a Claude question screen, whole or clipped, is never a prompt", () => {
    const screens = [
      ["←  ☐ Color  ☐ Size  ✔ Submit  →", "Pick a color?", "❯ 1. Red", "  2. Blue", "  3. Type something.", "  4. Chat about this", "Enter to select · Esc to cancel"],
      ["  4. Chat about this", "Enter to select · Tab/Arrow keys to navigate · Esc to cancel"],
    ];
    for (const lastOutputLines of screens) {
      expect(
        buildTerminalActionPrompt({
          status: "blocked",
          provider: "claude",
          approvals: [approval("a1", "AskUserQuestion")],
          lastOutputLines,
        }),
      ).toBeNull();
    }
  });

  test("a Claude permission menu is still shown read-only", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      provider: "claude",
      lastOutputLines: ["Do you want to proceed?", "❯ 1. Yes", "  2. No, and tell Claude what to do differently"],
    });
    expect(prompt?.actionable).toBe(false);
    expect(prompt?.options.map((option) => option.id)).toEqual(["1", "2"]);
  });

  test("projects Grok weekly-limit choice menu as display-only Interface state", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      provider: "grok",
      summary: "↑/↓ navigate · y copy Enter:submit",
      lastOutputLines: WEEKLY_LIMIT_LINES,
    });

    expect(prompt).not.toBeNull();
    expect(prompt?.actionable).toBe(false);
    expect(prompt?.title).toBe("You hit your weekly limit.");
    expect(prompt?.options.map((option) => option.label)).toEqual([
      "Upgrade tier",
      "Buy more credits",
    ]);
    expect(prompt?.options.map((option) => option.description)).toEqual([
      "Upgrade to a higher tier for more usage",
      "Purchase credits to keep using Grok Build",
    ]);
    expect(prompt?.defaultOptionId).toBeUndefined();
    expect(prompt?.options.every((option) => !option.default && !option.primary)).toBe(true);
    expect(prompt?.inputHints).toContain("navigate");
    expect(prompt?.inputHints?.toLowerCase()).toContain("enter");
  });

  test("preserves proven Grok selection caret without fabricating others", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      provider: "grok",
      lastOutputLines: [
        "You hit your free usage limit.",
        "› 1 ○ Upgrade to SuperGrok",
        "  2 ○ Continue free",
        "↑/↓ navigate · enter confirm · esc cancel",
      ],
    });

    expect(prompt?.actionable).toBe(false);
    expect(prompt?.defaultOptionId).toBe("1");
    expect(prompt?.options[0]?.default).toBe(true);
    expect(prompt?.options[1]?.default).toBe(false);
  });

  test("preserves proven filled radio glyph without fabricating others", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      provider: "grok",
      lastOutputLines: [
        "Choose a plan",
        "1 ● Upgrade to SuperGrok",
        "2 ○ Continue free",
        "↑/↓ navigate · enter confirm · esc cancel",
      ],
    });

    expect(prompt?.actionable).toBe(false);
    expect(prompt?.defaultOptionId).toBe("1");
  });

  test("does not invent selection when multiple filled markers appear", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      provider: "grok",
      lastOutputLines: [
        "Choose a plan",
        "1 ● Upgrade to SuperGrok",
        "2 ● Continue free",
        "↑/↓ navigate · enter confirm · esc cancel",
      ],
    });

    expect(prompt?.defaultOptionId).toBeUndefined();
    expect(prompt?.options.every((option) => !option.default)).toBe(true);
  });

  test("clears when blocked status resolves (stale-session safeguard)", () => {
    expect(
      buildTerminalActionPrompt({
        status: "running",
        provider: "grok",
        lastOutputLines: WEEKLY_LIMIT_LINES,
      }),
    ).toBeNull();
    expect(
      buildTerminalActionPrompt({
        status: "unknown",
        provider: "grok",
        lastOutputLines: WEEKLY_LIMIT_LINES,
      }),
    ).toBeNull();
  });

  test("clears when pane lines no longer contain a choice menu", () => {
    expect(
      buildTerminalActionPrompt({
        status: "blocked",
        provider: "grok",
        lastOutputLines: [
          "│ ❯                                                                        │",
          "╰─────────────────────────────────────── Grok 4.5 (high) · always-approve ─╯",
          "Shift+Tab:mode  │  Ctrl+c:cancel  │  Ctrl+g:send to bg  │  Ctrl+x:shortcuts",
        ],
      }),
    ).toBeNull();
  });

  test("does not invent a card from options without navigate footer", () => {
    expect(
      buildTerminalActionPrompt({
        status: "blocked",
        provider: "grok",
        lastOutputLines: [
          "1 (O) Upgrade tier",
          "2 (O) Buy more credits",
          "Shift+Tab:mode  │  Ctrl+c:cancel",
        ],
      }),
    ).toBeNull();
  });

  test("does not invent a card from navigate footer without numbered choices", () => {
    expect(
      buildTerminalActionPrompt({
        status: "blocked",
        provider: "grok",
        lastOutputLines: [
          "Resume session",
          "↑/↓ navigate · enter confirm · esc cancel",
        ],
      }),
    ).toBeNull();
  });

  test("keeps Codex approval prompts actionable", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      provider: "codex",
      lastOutputLines: [
        "$ systemctl list-unit-files --state=enabled",
        "› 1. Yes, proceed (y)",
        "  2. No, and tell Codex what to do differently (esc)",
        "Press enter to confirm or esc to cancel",
      ],
    });

    expect(prompt?.actionable).toBe(true);
    expect(prompt?.title).toBe("Waiting for confirmation");
    expect(prompt?.options[0]?.key).toBe("y");
    expect(prompt?.defaultOptionId).toBe("1");
  });

  test("does not invent a card from Grok retry_state / 402 text alone", () => {
    expect(
      buildTerminalActionPrompt({
        status: "blocked",
        provider: "grok",
        summary: "Provider request failed",
        lastOutputLines: [
          "API error (status 402 Payment Required): Grok Build usage balance exhausted",
          "Request URL: https://cli-chat-proxy.grok.com/v1/responses",
        ],
      }),
    ).toBeNull();
  });

  for (const provider of ["codex", "claude", "cursor", "terminal", ""]) {
    test(`does not project Grok card under non-Grok provider (${provider || "unknown"})`, () => {
      expect(
        buildTerminalActionPrompt({
          status: "blocked",
          provider,
          lastOutputLines: WEEKLY_LIMIT_LINES,
        }),
      ).toBeNull();
    });
  }

  test("scopes prompt id to the live process/session generation", () => {
    const first = buildTerminalActionPrompt({
      status: "blocked",
      provider: "grok",
      scopeKey: liveActionPromptScopeKey({
        workerId: "agent-a",
        processId: 11,
        startedAt: 100,
        connectionGeneration: 1,
      }),
      lastOutputLines: WEEKLY_LIMIT_LINES,
    });
    const second = buildTerminalActionPrompt({
      status: "blocked",
      provider: "grok",
      scopeKey: liveActionPromptScopeKey({
        workerId: "agent-a",
        processId: 22,
        startedAt: 200,
        connectionGeneration: 2,
      }),
      lastOutputLines: WEEKLY_LIMIT_LINES,
    });

    expect(first?.id.startsWith("agent-a:11:100:1|")).toBe(true);
    expect(second?.id.startsWith("agent-a:22:200:2|")).toBe(true);
    expect(first?.id).not.toBe(second?.id);
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
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)?.title).toBe(
      "You hit your weekly limit.",
    );
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
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)?.actionable).toBe(false);

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
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)?.title).toBe(
      "You hit your weekly limit.",
    );
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
    expect(
      projectPrompt(slice, "server", {
        id: "grok-1",
        status: "unknown",
        provider: "grok",
        lastOutputLines: [
          "│ ❯",
          "╰─────────────────────────────────────── Grok 4.5 (high) · always-approve ─╯",
        ],
      }),
    ).toBeNull();
  });
});

describe("Claude terminal prompts", () => {

  test("plan approval is read-only with Claude's own labels", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      provider: "claude",
      summary: "Waiting for plan approval",
      lastOutputLines: [
        " Claude has written up a plan and is ready to execute. Would you like to proceed?",
        " ❯ 1. Yes, and use auto mode",
        "   2. Yes, manually approve edits",
        "   3. Tell Claude what to change",
        "      shift+tab to approve with this feedback",
        " ctrl+g to edit in VS Code · ~/.claude/plans/plan.md",
      ],
    });
    expect(prompt?.title).toBe("Waiting for plan approval");
    expect(prompt?.actionable).toBe(false);
    expect(prompt?.options.map((option) => option.label)).toEqual([
      "Yes, and use auto mode",
      "Yes, manually approve edits",
      "Tell Claude what to change",
    ]);
    expect(prompt?.defaultOptionId).toBe("1");
  });

  test("permission prompt never relabels Claude options as Codex actions", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      provider: "claude",
      summary: "Waiting for Claude permission",
      lastOutputLines: [
        " Do you want to proceed?",
        " ❯ 1. Yes",
        "   2. Yes, and switch to auto mode · auto mode handles these prompts for you",
        "   3. No",
        " Esc to cancel · Tab to amend",
      ],
    });
    expect(prompt?.actionable).toBe(false);
    expect(prompt?.detail).not.toContain("Codex");
    expect(prompt?.options.map((option) => option.label)).toEqual([
      "Yes",
      "Yes, and switch to auto mode · auto mode handles these prompts for you",
      "No",
    ]);
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

  test("answers through the agent and reads nothing off the screen", () => {
    const prompt = buildTerminalActionPrompt({
      status: "running",
      lastOutputLines: ["1. Yes, proceed", "2. No"],
      provider: "codex",
      approvals: [approval],
      scopeKey: "w|1",
    });
    expect(prompt?.id).toBe("w|1|approval:a1");
    expect(prompt?.approvalId).toBe("a1");
    expect(prompt?.requestText).toBe("touch /tmp/x");
    expect(prompt?.options.map((o) => [o.id, o.approvalOptionId, o.key])).toEqual([
      ["accept", "accept", ""],
      ["cancel", "cancel", ""],
    ]);
    expect(prompt?.options[0]).toMatchObject({ primary: true, default: true });
    expect(prompt?.options[1].destructive).toBe(true);
    expect(prompt?.defaultOptionId).toBe("accept");
  });

  test("a Worker waiting on nothing is not blocked by what the screen shows", () => {
    expect(
      buildTerminalActionPrompt({ status: "running", lastOutputLines: ["1. Yes", "2. No"], provider: "codex" }),
    ).toBeNull();
  });
});
