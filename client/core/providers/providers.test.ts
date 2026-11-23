import { describe, expect, test } from "bun:test";
import {
  parseProviderConnectionTestResult,
  parseProviderModel,
  parseProvidersSnapshot,
  parseThreadRuntimeSelection,
} from "./parse";
import {
  advancedConnectionInput,
  createdConnectionFromMutation,
  enabledModelIds,
  modelSupportToggleTurnsOff,
  toggleModelSupport,
} from "./presentation";
import { ProviderRequestOwner } from "./reconcile";
import type { ProvidersSnapshot } from "./types";

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

describe("Provider DTO parse", () => {
  test("keeps catalog freshness and capability metadata secret-free", () => {
    const model = parseProviderModel({
      id: "gpt-5.5",
      available: true,
      source: "discovered",
      context_window_tokens: 272000,
      modalities: ["text", "image"],
      temperature_supported: false,
      input_price_per_million: 1.25,
    });
    expect(model).toMatchObject({
      id: "gpt-5.5",
      context_window_tokens: 272000,
      modalities: ["text", "image"],
      temperature_supported: false,
      input_price_per_million: 1.25,
    });
    const snapshot = parseProvidersSnapshot({
      revision: 1,
      connections: [
        {
          id: "c1",
          name: "Gateway",
          clients: ["codex"],
          credential_ready: true,
          models_fetched_at: "2026-09-24T00:00:00Z",
          models_stale: true,
          models_warning: "using last known good models",
        },
      ],
      defaults: {},
      presets: [],
      models: { c1: [model] },
    });
    expect(snapshot?.connections[0]).toMatchObject({
      models_fetched_at: "2026-09-24T00:00:00Z",
      models_stale: true,
    });
  });

  test("parses curated catalog without leaking secret keys", () => {
    const catalog = parseProvidersSnapshot({
      revision: 3,
      connections: [
        {
          id: "c1",
          name: "DeepSeek",
          preset_id: "deepseek",
          clients: ["codex"],
          credential_ready: true,
        },
      ],
      defaults: {
        codex: { connection_id: "c1" },
      },
      presets: [
        { id: "deepseek", label: "DeepSeek", clients: ["codex", "claude"] },
        {
          id: "custom",
          label: "Custom Gateway",
          clients: ["codex", "claude"],
          advanced: true,
        },
      ],
      models: {
        c1: [{ id: "deepseek-chat", available: true, source: "bundled" }],
      },
    });
    expect(catalog?.revision).toBe(3);
    expect(catalog?.connections[0]?.name).toBe("DeepSeek");
    expect(catalog?.connections[0]?.base_url).toBeUndefined();
  });

  test("preserves an explicit direct-login connection-only default", () => {
    const catalog = parseProvidersSnapshot({
      revision: 4,
      connections: [
        {
          id: "c1",
          name: "Gateway",
          clients: ["codex"],
          credential_ready: true,
        },
      ],
      defaults: { codex: { connection_id: "" } },
      presets: [],
      models: { c1: [] },
    });
    expect(catalog?.defaults.codex).toEqual({
      connection_id: "",
    });
    expect(
      parseProvidersSnapshot({
        revision: 4,
        connections: [],
        defaults: { codex: { connection_id: "", model_id: "stray" } },
        presets: [],
        models: {},
      }),
    ).toBeNull();
  });

  test("rejects credential keys in catalog payloads", () => {
    expect(() =>
      parseProvidersSnapshot({
        revision: 1,
        connections: [
          {
            id: "c1",
            name: "X",
            clients: ["codex"],
            credential_ready: false,
            credential: "sk-should-never-parse",
          },
        ],
        defaults: {},
        presets: [],
        models: {},
      }),
    ).toThrow(/credential/i);
  });

  test("rejects old shared connections without one client owner", () => {
    const raw = providerSnapshot();
    raw.connections[0] = {
      ...raw.connections[0]!,
      clients: ["codex", "claude"],
    };
    expect(parseProvidersSnapshot(raw)).toBeNull();
  });

  test("parses session selection without secrets", () => {
    const selection = parseThreadRuntimeSelection({
      session_id: "mh-1",
      client: "codex",
      connection_id: "c1",
      connection_name: "DeepSeek",
      model_id: "deepseek-chat",
      credential_ready: true,
      hot_switchable: true,
    });
    expect(selection?.connection_id).toBe("c1");
    expect(selection?.reasoning_efforts).toBeUndefined();
  });

  test("any agent's selection that carries its own models parses; without them only Provider clients do", () => {
    const own = {
      session_id: "mh-1",
      client: "opencode",
      connection_id: "opencode",
      connection_name: "OpenCode",
      model_id: "opencode-go/deepseek-v4.1-flash",
      credential_ready: true,
      hot_switchable: true,
      native_models: [{ id: "opencode-go/deepseek-v4.1-flash", available: true, source: "opencode_native", reasoning_efforts: ["low", "high"] }],
    };
    expect(parseThreadRuntimeSelection(own)?.native_models?.[0]?.reasoning_efforts).toEqual(["low", "high"]);
    expect(parseThreadRuntimeSelection({ ...own, native_models: undefined })).toBeNull();
  });

  test("session selection parses the Reasoning Effort projection", () => {
    const selection = parseThreadRuntimeSelection({
      session_id: "mh-1",
      client: "codex",
      connection_id: "c1",
      connection_name: "Gateway",
      model_id: "gpt-5.6-sol",
      reasoning_effort: "high",
      reasoning_effort_default: "medium",
      reasoning_efforts: ["low", "medium", "high", "xhigh", "max"],
      credential_ready: true,
      hot_switchable: true,
    });
    expect(selection?.reasoning_effort).toBe("high");
    expect(selection?.reasoning_effort_default).toBe("medium");
    expect(selection?.reasoning_efforts).toEqual([
      "low",
      "medium",
      "high",
      "xhigh",
      "max",
    ]);
  });

  test("model entries parse the daemon-known flag", () => {
    const parsed = parseProviderModel({
      id: "gateway-only-model",
      available: true,
      source: "discovered",
    });
    expect(parsed?.known).toBeUndefined();
    const known = parseProviderModel({
      id: "gpt-5.6-sol",
      available: true,
      source: "discovered",
      known: true,
    });
    expect(known?.known).toBe(true);
  });

  test("parses a secret-free connection test result", () => {
    expect(
      parseProviderConnectionTestResult(
        { client: "codex", model_count: 7, latency_ms: 123 },
        "codex",
      ),
    ).toEqual({ client: "codex", modelCount: 7, latencyMs: 123 });
    expect(
      parseProviderConnectionTestResult(
        { client: "claude", model_count: 7, latency_ms: 123 },
        "codex",
      ),
    ).toBeNull();
    expect(
      parseProviderConnectionTestResult(
        { client: "codex", model_count: 7 },
        "codex",
      ),
    ).toBeNull();
    expect(() =>
      parseProviderConnectionTestResult(
        {
          client: "codex",
          model_count: 7,
          latency_ms: 123,
          credential: "sk-leak",
        },
        "codex",
      ),
    ).toThrow(/credential/i);
  });
});

describe("Provider Settings and Plus presentation policy", () => {
  test("custom endpoints are explicitly scoped to one client", () => {
    const gateway = advancedConnectionInput({
      name: "Internal",
      client: "codex",
      baseUrl: "https://gateway.example/v1",
    });
    expect(gateway).toEqual({
      name: "Internal",
      preset_id: "custom",
      client: "codex",
      base_url: "https://gateway.example/v1",
      advanced: true,
    });
    expect(gateway.client).toBe("codex");
    expect(gateway.model_id).toBeUndefined();
    expect(() =>
      advancedConnectionInput({
        name: "Bad",
        client: "codex",
        baseUrl: "file:///tmp/provider",
      }),
    ).toThrow(/HTTP or HTTPS/i);
  });

  test("create credential admission requires one unambiguous new connection", () => {
    const before = providerSnapshot();
    const created = {
      ...before,
      revision: 2,
      connections: [
        ...before.connections,
        {
          id: "c3",
          name: "DeepSeek 2",
          preset_id: "deepseek",
          clients: ["codex"],
          credential_ready: false,
          advanced: false,
        },
      ],
      models: { ...before.models, c3: [] },
    };
    expect(createdConnectionFromMutation(before, created, "deepseek").id).toBe(
      "c3",
    );

    const ambiguous = {
      ...created,
      connections: [
        ...created.connections,
        {
          id: "c4",
          name: "DeepSeek 3",
          preset_id: "deepseek",
          clients: ["codex"],
          credential_ready: false,
          advanced: false,
        },
      ],
      models: { ...created.models, c4: [] },
    };
    expect(() =>
      createdConnectionFromMutation(before, ambiguous, "deepseek"),
    ).toThrow(/uniquely identify/i);
  });
});

describe("Provider stale, reconnect, and ambiguity admission", () => {
  test("current-server rebind invalidates stale catalog and Session tokens", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a", "agent-a");
    const catalog = owner.admitCatalogLoad();
    const session = owner.admitSessionLoad();
    expect(catalog.ok).toBe(true);
    expect(session.ok).toBe(true);
    if (!catalog.ok || !session.ok) return;

    owner.rebind("server-b", "agent-b");
    expect(owner.isCurrent(catalog.token)).toBe(false);
    expect(owner.isCurrent(session.token)).toBe(false);
    expect(owner.acceptCatalog(catalog.token, 99)).toBe(false);
    expect(owner.acceptSession(session.token)).toBe(false);
  });

  test("older catalog revisions cannot overwrite the applied projection", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a");
    const first = owner.admitCatalogLoad();
    expect(first.ok).toBe(true);
    if (!first.ok) return;
    expect(owner.acceptCatalog(first.token, 8)).toBe(true);

    const stale = owner.admitCatalogLoad();
    expect(stale.ok).toBe(true);
    if (!stale.ok) return;
    expect(owner.acceptCatalog(stale.token, 7)).toBe(false);
  });

  test("ambiguous catalog and runtime-switch results lock until successful refresh", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a", "agent-a");
    const mutation = owner.admitCatalogMutation();
    expect(mutation.ok).toBe(true);
    if (!mutation.ok) return;
    owner.settleCatalogMutation(mutation.token, { refreshRequired: true });
    expect(owner.admitCatalogMutation()).toMatchObject({ ok: false });

    const refresh = owner.admitCatalogLoad();
    expect(refresh.ok).toBe(true);
    if (!refresh.ok) return;
    expect(owner.acceptCatalog(refresh.token, 2)).toBe(true);
    expect(owner.admitCatalogMutation()).toMatchObject({ ok: true });

    // Use a fresh owner so the admitted catalog write above does not overlap.
    const sessionOwner = new ProviderRequestOwner();
    sessionOwner.rebind("server-a", "agent-a");
    const runtimeSwitch = sessionOwner.admitRuntimeSwitch();
    expect(runtimeSwitch.ok).toBe(true);
    if (!runtimeSwitch.ok) return;
    sessionOwner.settleRuntimeSwitch(runtimeSwitch.token, {
      refreshRequired: true,
    });
    expect(sessionOwner.admitRuntimeSwitch()).toMatchObject({ ok: false });
    const sessionRefresh = sessionOwner.admitSessionLoad();
    expect(sessionRefresh.ok).toBe(true);
    if (!sessionRefresh.ok) return;
    expect(sessionOwner.acceptSession(sessionRefresh.token)).toBe(true);
    expect(sessionOwner.admitRuntimeSwitch()).toMatchObject({ ok: true });
  });
});

describe("Model sync and default binding policy", () => {
  test("support toggle flips one model and keeps catalog order", () => {
    const snapshot = providerSnapshot({
      models: {
        c1: [
          { id: "gpt-5.6-sol", available: true, source: "discovered" },
          { id: "gpt-5.4-mini", available: true, source: "discovered" },
          { id: "retired-model", available: false, source: "lkg" },
        ],
      },
    });
    expect(enabledModelIds(snapshot, "c1")).toEqual([
      "gpt-5.6-sol",
      "gpt-5.4-mini",
    ]);
    expect(toggleModelSupport(snapshot, "c1", "gpt-5.6-sol")).toEqual([
      "gpt-5.4-mini",
    ]);
    const reenabled = toggleModelSupport(
      {
        ...snapshot,
        models: {
          c1: snapshot.models.c1!.map((m) => ({ ...m, available: false })),
        },
      },
      "c1",
      "gpt-5.6-sol",
    );
    expect(reenabled).toEqual(["gpt-5.6-sol"]);
  });

  test("only turning a model off needs a confirmation", () => {
    const snapshot = providerSnapshot({
      models: {
        c2: [
          { id: "claude-opus-5-5", available: true, source: "discovered" },
          { id: "claude-opus-4-8", available: false, source: "discovered" },
        ],
      },
    });
    expect(modelSupportToggleTurnsOff(snapshot, "c2", "claude-opus-5-5")).toBe(true);
    expect(modelSupportToggleTurnsOff(snapshot, "c2", "claude-opus-4-8")).toBe(false);
    expect(modelSupportToggleTurnsOff(null, "c2", "claude-opus-5-5")).toBe(false);
  });
});
