import { describe, expect, test } from "bun:test";
import { buildChatComposerPlaceholder, chatAgentSupportsSlashCommands } from "./chatComposerPresentation";

// The first two from the Expo app's services/connectionLifecycle.test.ts; the rest pin the file's other rules.
describe("composer placeholder", () => {
  test("connecting keeps composer available messaging (not unavailable)", () => {
    expect(
      buildChatComposerPlaceholder({
        agentKind: "codex",
        connectionState: "connecting",
        slashQueryActive: false,
      }),
    ).toBe("Connecting…");
  });

  test("true offline shows unavailable composer", () => {
    expect(
      buildChatComposerPlaceholder({
        agentKind: "codex",
        connectionState: "offline",
        slashQueryActive: false,
      }),
    ).toBe("Connection unavailable");
  });

  test("names the agent, searches commands only for Codex, and an explicit placeholder wins", () => {
    const connected = { connectionState: "connected" as const, slashQueryActive: false };
    expect(buildChatComposerPlaceholder({ ...connected, agentKind: "claude" })).toBe("Ask Claude");
    expect(buildChatComposerPlaceholder({ ...connected, agentKind: "terminal" })).toBe("Message");
    expect(buildChatComposerPlaceholder({ ...connected, agentKind: "codex", slashQueryActive: true })).toBe("Search commands");
    expect(buildChatComposerPlaceholder({ ...connected, agentKind: "claude", slashQueryActive: true })).toBe("Ask Claude");
    expect(buildChatComposerPlaceholder({ ...connected, agentKind: "codex", explicitPlaceholder: " Reply " })).toBe("Reply");
    expect(chatAgentSupportsSlashCommands("codex")).toBe(true);
    expect(chatAgentSupportsSlashCommands("claude")).toBe(false);
  });
});
