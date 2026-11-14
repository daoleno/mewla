import { describe, expect, test } from "bun:test";
import { outsideAgentKind } from "./outsideAgents";

describe("an outside agent's logo", () => {
  test("follows the name its client gave", () => {
    expect(outsideAgentKind("claude-code")).toBe("claude");
    expect(outsideAgentKind("Claude Code")).toBe("claude");
    expect(outsideAgentKind("codex-mcp-client")).toBe("codex");
    expect(outsideAgentKind("opencode")).toBe("opencode");
  });

  test("an unknown client has none: it keeps the generic tile", () => {
    expect(outsideAgentKind("my-script")).toBeNull();
  });
});
