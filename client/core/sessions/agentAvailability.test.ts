import { describe, expect, test } from "bun:test";
import { agentInstalled, missingAgentSentence } from "./agentAvailability";
import { ANTIGRAVITY_COMMAND, CLAUDE_CODE_COMMAND, CURSOR_AGENT_COMMAND } from "./agentCommands";

const executors = [
  { id: "claude", name: "claude", provider: "claude", command: "claude" },
  { id: "agent", name: "agent", provider: "cursor", command: "cursor-agent --force --sandbox disabled" },
  { id: "agy", name: "agy", provider: "antigravity", command: "agy" },
];

describe("an agent the computer found", () => {
  test("is told by the executor that runs its program", () => {
    const states = { claude: { installed: true, signed_in: true }, agent: { installed: true, signed_in: false } };
    expect(agentInstalled(CLAUDE_CODE_COMMAND, executors, states)).toBe(true);
    expect(agentInstalled(CURSOR_AGENT_COMMAND, executors, states)).toBe(true);
    expect(agentInstalled(ANTIGRAVITY_COMMAND, executors, states)).toBe(false);
  });

  test("is unknown, so never stopped, for a shell, a program no executor runs, or a snapshot that says nothing", () => {
    const states = { claude: { installed: true, signed_in: true } };
    expect(agentInstalled("", executors, states)).toBe(true);
    expect(agentInstalled("pi", executors, states)).toBe(true);
    expect(agentInstalled(ANTIGRAVITY_COMMAND, executors, undefined)).toBe(true);
    expect(agentInstalled(ANTIGRAVITY_COMMAND, undefined, states)).toBe(true);
  });
});

describe("the refusal sentence", () => {
  test("names the agent and the computer as the header does", () => {
    expect(missingAgentSentence("Antigravity", "workbench")).toBe("Antigravity is not installed on workbench.");
    expect(missingAgentSentence("Codex", undefined)).toBe("Codex is not installed on this computer.");
  });
});
