import { describe, expect, test } from "bun:test";
import {
  effectRowsForRuntime,
  resolveComposerModelControl,
  runtimeChoiceForRow,
  runtimeSwitchNote,
  threadRuntimeRows,
} from "./sessionModelHelpers";
import type { ThreadRuntimeSelection, ProvidersSnapshot } from "../providers/types";

const selection: ThreadRuntimeSelection = {
  session_id: "s1",
  client: "codex",
  connection_id: "a",
  connection_name: "Alpha",
  model_id: "gpt-5.4",
  reasoning_effort: "high",
  reasoning_effort_default: "medium",
  reasoning_efforts: ["low", "medium", "high"],
  credential_ready: true,
  hot_switchable: true,
};

const snapshot: ProvidersSnapshot = {
  revision: 1,
  defaults: { codex: { connection_id: "b" } },
  presets: [],
  connections: [
    { id: "a", name: "Alpha", clients: ["codex"], credential_ready: true, advanced: false },
    { id: "b", name: "Beta", clients: ["codex"], credential_ready: true, advanced: false },
  ],
  models: {
    a: [{ id: "gpt-5.4", available: true, source: "bundled", reasoning_effort_default: "medium", reasoning_efforts: ["low", "medium", "high"] }],
    b: [{ id: "gpt-5.5", available: true, source: "bundled", reasoning_effort_default: "medium", reasoning_efforts: ["low", "medium", "high", "xhigh"] }],
  },
};

describe("thread runtime picker model", () => {
  test("Settings default mismatch never changes acknowledged current runtime", () => {
    const rows = threadRuntimeRows({ snapshot, selection });
    expect(rows.find((row) => row.current)?.key).toBe("a:gpt-5.4");
    expect(rows.map((row) => row.connectionId)).toEqual(["a"]);
    expect(resolveComposerModelControl({
      capabilities: {
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: true,
      },
      connectionConnected: true,
      selection,
      refreshRequired: false,
    })?.label).toBe("gpt-5.4");
    expect(resolveComposerModelControl({
      capabilities: {
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: true,
      },
      connectionConnected: true,
      selection,
      refreshRequired: false,
    })?.accessibilityLabel).toContain("Open model and effect");
  });

  test("runtime choice is atomic and rejects unsupported effects", () => {
    const target = {
      key: "b:gpt-5.5",
      connectionId: "b",
      connectionName: "Beta",
      modelId: "gpt-5.5",
      label: "gpt-5.5",
      current: false,
      disabled: false,
      unsupported: false,
      unavailableCurrent: false,
      effectDefault: "medium",
      effects: ["low", "medium", "high", "xhigh"],
      currentEffect: "",
    };
    expect(runtimeChoiceForRow(target, "xhigh")).toEqual({
      connectionId: "b",
      modelId: "gpt-5.5",
      effect: "xhigh",
    });
    expect(runtimeChoiceForRow(target, "")).toEqual({
      connectionId: "b",
      modelId: "gpt-5.5",
      useDefaultEffect: true,
    });
    expect(runtimeChoiceForRow(target, "turbo")).toBeNull();
  });

  test("known Codex models expose Default plus exact effect metadata", () => {
    const rows = threadRuntimeRows({
      snapshot,
      selection: { ...selection, reasoning_effort: undefined },
    });
    const row = rows.find((candidate) => candidate.current);
    expect(row?.effects).toEqual(["", "low", "medium", "high"]);
    expect(row?.currentEffect).toBe("");
    expect(row?.disabled).toBe(false);
  });

  test("unknown Codex models remain selectable with Default only", () => {
    const unknownSnapshot: ProvidersSnapshot = {
      ...snapshot,
      models: {
        a: [{
          id: "vendor/private-alpha",
          available: true,
          source: "discovered",
          known: false,
        }],
      },
    };
    const rows = threadRuntimeRows({
      snapshot: unknownSnapshot,
      selection: { ...selection, model_id: "vendor/private-alpha", reasoning_effort: undefined, reasoning_effort_default: undefined, reasoning_efforts: undefined },
    });
    expect(rows).toHaveLength(1);
    expect(rows[0]?.disabled).toBe(false);
    expect(rows[0]?.unsupported).toBe(false);
    expect(rows[0]?.effects).toEqual([""]);
  });

  test("daemon display metadata labels the exact model slug", () => {
    const rows = threadRuntimeRows({
      snapshot: {
        ...snapshot,
        models: {
          a: [{
            id: "gpt-5.6",
            display_name: "GPT 5.6 Preview",
            available: true,
            source: "codex_cache",
          }],
        },
      },
      selection: { ...selection, model_id: "gpt-5.6" },
    });
    expect(rows[0]?.modelId).toBe("gpt-5.6");
    expect(rows[0]?.label).toBe("GPT 5.6 Preview");
  });
});

describe("an agent's own models", () => {
  const claudeProvider: ThreadRuntimeSelection = { ...selection, client: "claude", model_id: "claude-sonnet-5-5" };
  const claudeSnapshot: ProvidersSnapshot = {
    ...snapshot,
    connections: [{ id: "a", name: "Alpha", clients: ["claude"], credential_ready: true, advanced: false }],
    models: { a: [{ id: "claude-sonnet-5-5", available: true, source: "bundled", reasoning_efforts: ["low", "high"] }] },
  };

  test("any agent's runtime that carries its models lists them with each model's own efforts", () => {
    const own: ThreadRuntimeSelection = {
      ...claudeProvider,
      connection_id: "claude",
      connection_name: "Claude Code",
      model_id: "sonnet",
      reasoning_effort: "high",
      native_models: [
        { id: "default", display_name: "Default (recommended)", available: true, source: "claude_native", reasoning_efforts: ["low", "medium", "high", "xhigh", "max"] },
        { id: "sonnet", display_name: "Sonnet", available: true, source: "claude_native", reasoning_efforts: ["low", "medium", "high"] },
      ],
    };
    const ownSnapshot: ProvidersSnapshot = {
      revision: 0, presets: [], defaults: {},
      connections: [{ id: "claude", name: "Claude Code", clients: ["claude"], credential_ready: true, advanced: false }],
      models: { claude: own.native_models ?? [] },
    };
    const rows = threadRuntimeRows({ snapshot: ownSnapshot, selection: own });
    expect(rows.map((row) => row.label)).toEqual(["Default (recommended)", "Sonnet"]);
    const current = rows.find((row) => row.current);
    expect(current?.modelId).toBe("sonnet");
    expect(current?.effects).toEqual(["", "low", "medium", "high"]);
    expect(current?.currentEffect).toBe("high");
    expect(resolveComposerModelControl({
      capabilities: { structured_events: true, model_profile_managed: true, model_profile_active_switch: true },
      connectionConnected: true,
      selection: own,
      refreshRequired: false,
    })?.label).toBe("Sonnet");
  });

  test("an agent that always runs one of its levels offers no Default, and a held pick is marked", () => {
    // 11-24: Pi's sheet offered "Default"; Pi kept its level, so the switch "failed".
    const pi: ThreadRuntimeSelection = {
      ...claudeProvider,
      client: "pi",
      connection_id: "pi",
      connection_name: "Pi",
      model_id: "opencode-go/flash",
      reasoning_effort: "high",
      pending_model_id: "opencode-go/flash",
      pending_reasoning_effort: "low",
      native_models: [
        { id: "opencode-go/flash", available: true, source: "pi_native", reasoning_efforts: ["low", "high", "max"], reasoning_effort_required: true },
      ],
    };
    const piSnapshot: ProvidersSnapshot = {
      revision: 0, presets: [], defaults: {},
      connections: [{ id: "pi", name: "Pi", clients: ["pi"], credential_ready: true, advanced: false }],
      models: { pi: pi.native_models ?? [] },
    };
    const [row] = threadRuntimeRows({ snapshot: piSnapshot, selection: pi });
    expect(row.effects).toEqual(["low", "high", "max"]);
    expect(row.pending).toBe(true);
    expect(effectRowsForRuntime(row).map((entry) => [entry.effect, entry.selected, entry.pending])).toEqual([
      ["low", false, true], ["high", true, false], ["max", false, false],
    ]);
    const chip = resolveComposerModelControl({
      capabilities: { structured_events: true, model_profile_managed: true, model_profile_active_switch: true },
      connectionConnected: true, selection: pi, refreshRequired: false,
    });
    expect(chip?.pending).toBe(true);
  });

  test("a held pick says it applies after this turn, and one the agent did not take says why", () => {
    // 11-25: the sheet showed only an hourglass for a pick held through a long
    // turn, and nothing at all when the pick was not taken.
    const claude: ThreadRuntimeSelection = {
      ...claudeProvider,
      connection_id: "claude",
      connection_name: "Claude Code",
      model_id: "sonnet",
      native_models: [
        { id: "default", display_name: "Default · Opus 5.5", available: true, source: "claude_native", reasoning_efforts: ["low", "high"] },
        { id: "opus", display_name: "Opus 5.5", available: true, source: "claude_native", reasoning_efforts: ["low", "high"] },
        { id: "sonnet", display_name: "Sonnet 5.5", available: true, source: "claude_native", reasoning_efforts: ["low", "high"] },
      ],
    };
    expect(runtimeSwitchNote(claude)).toBeNull();
    expect(runtimeSwitchNote({ ...claude, pending_model_id: "opus" })).toEqual({ kind: "pending", text: "Opus 5.5 after this turn" });
    expect(runtimeSwitchNote({ ...claude, pending_model_id: "opus", pending_reasoning_effort: "high" })?.text).toBe("Opus 5.5, High effort after this turn");
    expect(runtimeSwitchNote({ ...claude, failed_model_id: "opus", failed_reason: "Claude's model picker did not show as expected" })).toEqual({
      kind: "failed",
      text: "Could not switch to Opus 5.5: Claude's model picker did not show as expected",
    });
    expect(resolveComposerModelControl({
      capabilities: { structured_events: true, model_profile_managed: true, model_profile_active_switch: true },
      connectionConnected: true, selection: { ...claude, pending_model_id: "opus" }, refreshRequired: false,
    })?.accessibilityLabel).toBe("Open model and effect, Opus 5.5 after this turn");
  });

  test("a Claude Session on a Provider connection still shows no efforts", () => {
    const rows = threadRuntimeRows({ snapshot: claudeSnapshot, selection: claudeProvider });
    expect(rows[0]?.current).toBe(true);
    expect(rows[0]?.effects).toEqual([]);
  });
});
