import { describe, expect, test } from "bun:test";
import { CLAUDE_CODE_COMMAND, commandBinary, findSupportedAgentMention, LAUNCH_PRESETS, stripSupportedAgentMentions } from "./agentCommands";

// The Expo app had no test for this file.
describe("agent commands", () => {
  test("the New session presets, in order, with their commands", () => {
    expect(LAUNCH_PRESETS.map((preset) => preset.key)).toEqual(["shell", "claude", "codex", "cursor", "grok", "pi", "opencode", "dsh"]);
    expect(LAUNCH_PRESETS[0].command).toBe("");
    expect(LAUNCH_PRESETS[1].command).toBe(CLAUDE_CODE_COMMAND);
  });

  test("commandBinary is the first word's basename", () => {
    expect(commandBinary("  /usr/bin/Python3 -m http.server")).toBe("python3");
    expect(commandBinary("'codex' exec")).toBe("codex");
    expect(commandBinary(undefined)).toBe("");
  });

  test("@mentions name an agent and are stripped from the text", () => {
    expect(findSupportedAgentMention("ask @agent to fix it")?.id).toBe("cursor");
    expect(stripSupportedAgentMentions("ask @codex  to fix it")).toBe("ask to fix it");
  });
});
