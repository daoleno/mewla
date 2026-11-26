import { describe, expect, test } from "bun:test";
import {
  providerEditorCanSave,
  providerEditorInitialBaseUrl,
  providerEditorInitialName,
  providerEditorRequiresBaseUrl,
  type ProvidersEditorState,
  providerEditorTakesKey,
} from "./editorModel";
import { parseLocalProviders } from "./parse";
import {
  providerBaseUrlHostname,
  providerNameIssue,
  localAddressLabel,
  presetsForClient,
} from "./presentation";
import type { ProvidersSnapshot } from "./types";

type ConnectionFixture = {
  id: string;
  name: string;
  preset_id: string;
  clients: string[];
  credential_ready: boolean;
  advanced: boolean;
  base_url?: string;
  manual_model_id?: string;
};

function connection(
  id = "conn-a",
  overrides: Partial<ConnectionFixture> = {},
): ConnectionFixture {
  return {
    id,
    name: "Alpha gateway",
    preset_id: "custom",
    clients: ["codex"],
    credential_ready: false,
    advanced: true,
    base_url: "https://api.example.com/v1",
    ...overrides,
  };
}

function snapshot(
  connections: ReturnType<typeof connection>[],
): ProvidersSnapshot {
  return {
    revision: 1,
    connections,
    defaults: {},
    presets: [
      { id: "openai", label: "OpenAI", clients: ["codex"], advanced: false },
      {
        id: "custom",
        label: "Custom Gateway",
        clients: ["codex", "claude"],
        advanced: true,
      },
    ],
    models: {},
  };
}

describe("unified Add/Edit Provider editor", () => {
  const base = {
    mutating: false,
    name: "Work gateway",
    baseUrl: "https://api.example.com/v1",
    apiKey: "sk-live",
    nameIssue: null as string | null,
    createMode: false,
    editMode: false,
    requiresBaseUrl: true,
  };

  test("create requires name, Base URL and API key", () => {
    expect(providerEditorCanSave({ ...base, createMode: true })).toBe(true);
    expect(
      providerEditorCanSave({ ...base, createMode: true, apiKey: "" }),
    ).toBe(false);
    expect(
      providerEditorCanSave({ ...base, createMode: true, baseUrl: "" }),
    ).toBe(false);
    expect(providerEditorCanSave({ ...base, createMode: true, name: "" })).toBe(
      false,
    );
  });

  test("edit may save with an empty API key (preserve stored secret)", () => {
    expect(providerEditorCanSave({ ...base, editMode: true, apiKey: "" })).toBe(
      true,
    );
  });

  test("a display-name issue blocks saving in every mode", () => {
    expect(
      providerEditorCanSave({
        ...base,
        createMode: true,
        nameIssue: "Another Provider is already named “Alpha gateway”.",
      }),
    ).toBe(false);
  });

  test("busy editor never saves", () => {
    expect(
      providerEditorCanSave({ ...base, createMode: true, mutating: true }),
    ).toBe(false);
  });

  test("edit prefills name and Base URL but never the API key", () => {
    const edit: ProvidersEditorState = {
      kind: "edit",
      connection: connection(),
    };
    expect(providerEditorInitialName(edit)).toBe("Alpha gateway");
    expect(providerEditorInitialBaseUrl(edit)).toBe(
      "https://api.example.com/v1",
    );
    expect(providerEditorRequiresBaseUrl(edit)).toBe(true);
    expect(
      providerEditorRequiresBaseUrl({ kind: "create", client: "codex" }),
    ).toBe(true);
  });

  test("curated connections hide the Base URL field", () => {
    const curated: ProvidersEditorState = {
      kind: "edit",
      connection: connection("conn-curated", {
        preset_id: "openai",
        advanced: false,
        base_url: undefined,
        name: "OpenAI",
      }),
    };
    expect(providerEditorRequiresBaseUrl(curated)).toBe(false);
    expect(providerEditorInitialBaseUrl(curated)).toBe("");
    expect(
      providerEditorCanSave({
        ...base,
        editMode: true,
        requiresBaseUrl: false,
        apiKey: "",
        name: "OpenAI",
      }),
    ).toBe(true);
  });
});

describe("Provider display-name helpers", () => {
  test("Base URL hostname is the secondary identity", () => {
    expect(providerBaseUrlHostname("https://api.example.com/v1")).toBe(
      "api.example.com",
    );
    expect(providerBaseUrlHostname("http://192.168.1.20:8080/v1")).toBe(
      "192.168.1.20",
    );
    expect(providerBaseUrlHostname("")).toBe("");
    expect(providerBaseUrlHostname("not a url")).toBe("not a url");
  });

  test("names are trimmed, bounded and case-insensitively unique", () => {
    const catalog = snapshot([connection("conn-a", { name: "Alpha gateway" })]);
    expect(providerNameIssue({ name: "  ", snapshot: catalog })).toMatch(
      /required/,
    );
    expect(
      providerNameIssue({ name: "x".repeat(65), snapshot: catalog }),
    ).toMatch(/64/);
    expect(
      providerNameIssue({ name: "alpha GATEWAY", snapshot: catalog }),
    ).toMatch(/already named/);
    expect(
      providerNameIssue({
        name: "alpha gateway",
        snapshot: catalog,
        exceptConnectionId: "conn-a",
      }),
    ).toBeNull();
    expect(
      providerNameIssue({ name: "Beta gateway", snapshot: catalog }),
    ).toBeNull();
    expect(
      providerNameIssue({ name: "Beta gateway", snapshot: null }),
    ).toBeNull();
  });
});

describe("Add Provider presets", () => {
  const presets = [
    { id: "kimi", label: "Kimi", clients: ["codex", "claude"], advanced: false },
    { id: "anthropic", label: "Anthropic", clients: ["claude"], advanced: false },
    { id: "ollama", label: "Ollama", clients: ["codex", "claude"], advanced: false, local: true, base_url: "http://localhost:11434" },
    { id: "openai", label: "OpenAI", clients: ["codex"], advanced: false },
    { id: "custom", label: "Custom Gateway", clients: ["codex", "claude"], advanced: true },
  ];
  const s = { revision: 1, connections: [], defaults: {}, presets, models: {} } as ProvidersSnapshot;

  test("each agent is offered its presets, vendors before local servers, Custom Gateway apart", () => {
    expect(presetsForClient(s, "claude").map((p) => p.id)).toEqual(["kimi", "anthropic", "ollama"]);
    expect(presetsForClient(s, "codex").map((p) => p.id)).toEqual(["kimi", "openai", "ollama"]);
    expect(presetsForClient(null, "codex")).toEqual([]);
  });

  test("a vendor asks for its key only; a local server for its address only", () => {
    const vendor = { kind: "create" as const, client: "claude" as const, preset: presets[0] };
    expect(providerEditorInitialName(vendor)).toBe("Kimi");
    expect(providerEditorRequiresBaseUrl(vendor)).toBe(false);
    expect(providerEditorTakesKey(vendor)).toBe(true);
    const local = { kind: "create" as const, client: "claude" as const, preset: presets[2] };
    expect(providerEditorInitialBaseUrl(local)).toBe("http://localhost:11434");
    expect(providerEditorInitialBaseUrl({ ...local, baseUrl: "http://localhost:11500" })).toBe("http://localhost:11500");
    expect(providerEditorRequiresBaseUrl(local)).toBe(true);
    expect(providerEditorTakesKey(local)).toBe(false);
    const fields = { mutating: false, name: "Ollama", baseUrl: "http://localhost:11434", apiKey: "", nameIssue: null, createMode: true, editMode: false, requiresBaseUrl: true };
    expect(providerEditorCanSave({ ...fields, keyless: true })).toBe(true);
    expect(providerEditorCanSave(fields)).toBe(false);
    // A Custom Gateway is as before: an address and a key.
    const custom = { kind: "create" as const, client: "codex" as const };
    expect(providerEditorInitialName(custom)).toBe("");
    expect(providerEditorRequiresBaseUrl(custom) && providerEditorTakesKey(custom)).toBe(true);
    // Editing a saved local connection asks no key either.
    const saved = { id: "c1", name: "Ollama", preset_id: "ollama", clients: ["claude"], credential_ready: true, advanced: true, base_url: "http://localhost:11434" };
    expect(providerEditorTakesKey({ kind: "edit", connection: saved }, presets)).toBe(false);
  });

  test("the daemon's local servers are read, and a malformed one is left out", () => {
    expect(parseLocalProviders({ type: "local_providers", found: [
      { preset_id: "ollama", label: "Ollama", base_url: "http://localhost:11434", models: 2, connection_ids: ["c1"] },
      { preset_id: "", base_url: "http://localhost:1" },
      "nonsense",
    ] })).toEqual([{ preset_id: "ollama", label: "Ollama", base_url: "http://localhost:11434", models: 2, connection_ids: ["c1"] }]);
    expect(parseLocalProviders({ type: "error" })).toEqual([]);
    expect(localAddressLabel("http://localhost:11434")).toBe("localhost:11434");
  });
});
