import { describe, expect, test } from "bun:test";
import { buildChatComposerPlaceholder } from "./chatComposerPresentation";

// The first two from the Expo app's services/connectionLifecycle.test.ts; the rest pin the file's other rules.
describe("composer placeholder", () => {
  test("connecting keeps composer available messaging (not unavailable)", () => {
    expect(
      buildChatComposerPlaceholder({
        agentKind: "codex",
        connectionState: "connecting",
      }),
    ).toBe("Connecting…");
  });

  test("true offline shows unavailable composer", () => {
    expect(
      buildChatComposerPlaceholder({
        agentKind: "codex",
        connectionState: "offline",
      }),
    ).toBe("Connection unavailable");
  });

  test("names the agent and an explicit placeholder wins", () => {
    const connected = { connectionState: "connected" as const };
    expect(buildChatComposerPlaceholder({ ...connected, agentKind: "claude" })).toBe("Message Claude");
    expect(buildChatComposerPlaceholder({ ...connected, agentKind: "antigravity" })).toBe("Message Antigravity");
    expect(buildChatComposerPlaceholder({ ...connected, agentKind: "terminal" })).toBe("Message");
    expect(buildChatComposerPlaceholder({ ...connected, agentKind: "codex" })).toBe("Message Codex");
    expect(buildChatComposerPlaceholder({ ...connected, agentKind: "codex", explicitPlaceholder: " Reply " })).toBe("Reply");
  });
});
