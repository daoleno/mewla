import { describe, expect, test } from "bun:test";
import {
  providerEditorAfterSave,
  providerEditorCanSave,
  providerEditorInitialBaseUrl,
  providerEditorInitialName,
  providerEditorRequiresBaseUrl,
  providerEditorSessionKey,
  providerEditorShouldResetFields,
  type ProvidersEditorState,
} from "./editorModel";
import {
  providerBaseUrlHostname,
  providerNameIssue,
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

  test("client identity is part of the editor target", () => {
    expect(providerEditorSessionKey({ kind: "create", client: "codex" })).toBe(
      "create:codex",
    );
    expect(providerEditorSessionKey({ kind: "create", client: "claude" })).toBe(
      "create:claude",
    );
    expect(
      providerEditorShouldResetFields("create:codex", "create:claude"),
    ).toBe(true);
    expect(
      providerEditorSessionKey({ kind: "edit", connection: connection() }),
    ).toBe("edit:conn-a");
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

  test("partial create success retries the same unified edit", () => {
    const created = connection("conn-created");
    const before: ProvidersEditorState = {
      kind: "create",
      client: "codex",
    };
    expect(
      providerEditorAfterSave(before, {
        status: "credential_failed",
        connection: created,
      }),
    ).toEqual({ kind: "edit", connection: created, retry: true });
    expect(
      providerEditorShouldResetFields("create:codex", "edit:conn-created"),
    ).toBe(false);
  });

  test("successful save closes the editor", () => {
    expect(
      providerEditorAfterSave(
        { kind: "create", client: "codex" },
        { status: "saved" },
      ),
    ).toBeNull();
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
