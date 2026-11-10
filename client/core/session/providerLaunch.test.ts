import { describe, expect, test } from "bun:test";
import type { ProvidersSnapshot } from "../sessions/providerTypes";
import { launchSelectionFromSnapshot, providerClientForCommand } from "./providerLaunch";

// From the Expo app's services/providers/providers.test.ts.
function providerSnapshot(
  overrides?: Partial<ProvidersSnapshot>,
): ProvidersSnapshot {
  return {
    revision: 1,
    connections: [
      {
        id: "c1",
        name: "DeepSeek",
        preset_id: "deepseek",
        clients: ["codex"],
        credential_ready: true,
        advanced: false,
      },
      {
        id: "c2",
        name: "Claude Gateway",
        preset_id: "custom",
        clients: ["claude"],
        credential_ready: false,
        advanced: true,
        base_url: "https://claude.example/v1",
      },
    ],
    defaults: {
      codex: { connection_id: "c1" },
    },
    presets: [
      {
        id: "deepseek",
        label: "DeepSeek",
        clients: ["codex"],
        advanced: false,
      },
      {
        id: "custom",
        label: "Custom Gateway",
        clients: ["codex", "claude"],
        advanced: true,
      },
    ],
    models: {
      c1: [
        { id: "deepseek-chat", available: true, source: "discovered" },
        { id: "retired", available: false, source: "lkg" },
      ],
      c2: [{ id: "claude-custom", available: true, source: "manual" }],
    },
    ...overrides,
  };
}

describe("Model sync and default binding policy", () => {
  test("launch selection carries the client's connection without a model seed", () => {
    const snapshot = providerSnapshot({
      defaults: { codex: { connection_id: "c1" } },
    });
    expect(providerClientForCommand("codex")).toBe("codex");
    expect(providerClientForCommand("cd /x && codex exec")).toBe("codex");
    expect(providerClientForCommand("claude -p hi")).toBe("claude");
    expect(providerClientForCommand("sh")).toBeNull();
    expect(launchSelectionFromSnapshot(snapshot, "codex")).toEqual({
      connectionId: "c1",
      modelId: "",
    });
    expect(launchSelectionFromSnapshot(snapshot, "claude")).toBeNull();
    expect(
      launchSelectionFromSnapshot(
        {
          ...snapshot,
          defaults: { claude: { connection_id: "c1" } },
        },
        "claude",
      ),
    ).toEqual({ connectionId: "c1", modelId: "" });
    expect(
      launchSelectionFromSnapshot({ ...snapshot, defaults: {} }, "codex"),
    ).toBeNull();
  });
});
