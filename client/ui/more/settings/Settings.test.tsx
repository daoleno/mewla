import { describe, expect, open, test } from "nsh/test";

const HARNESS = "../testing/Harness.tsx";

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

function computer(page: unknown, answer: (type: string, body: Record<string, unknown>) => unknown, extra: Record<string, unknown> = {}) {
  const asked: Array<{ type: string; body: Record<string, unknown> }> = [];
  return {
    asked,
    options: {
      ...extra,
      host: {
        params: { page },
        handlers: {
          ...secureStore(),
          "app.daemon": ({ type, body }: { type: string; body: Record<string, unknown> }) => {
            asked.push({ type, body });
            return answer(type, body) ?? { error: `unexpected ${type}` };
          },
        },
      },
    },
  };
}

const snapshot = (overrides: Record<string, unknown> = {}) => ({
  revision: 3,
  connections: [
    { id: "c1", name: "Work gateway", clients: ["codex"], credential_ready: true, advanced: true, base_url: "https://gw.example.com/v1", models_fetched_at: new Date().toISOString() },
    { id: "c2", name: "Spare", clients: ["codex"], credential_ready: false, advanced: true, base_url: "https://spare.example.com/v1" },
  ],
  defaults: { codex: { connection_id: "" } },
  presets: [],
  models: { c1: [{ id: "gpt-6", display_name: "GPT 6", available: true, source: "upstream" }, { id: "gpt-6-mini", available: false, source: "upstream" }] },
  ...overrides,
});

describe("Settings", () => {
  test("lists the computer, the channels, the agents, Labs, Appearance, Pet and About", async () => {
    const c = computer({ name: "settings" }, (type) => (type === "telegram_connection_status" ? { connection: { state: "connected", enabled: true, bot_username: "mewla_bot", owner_hint: "@me", binding_pending: false } } : null), {
      network: { "GET http://desk.test:9877/auth-check": { status: 503 } },
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:pair-another");
    await app.waitFor("text:@mewla_bot");
    const texts = app.texts();
    for (const word of ["Desk", "Current", "Telegram", "Model Providers", "Outside agents", "Friends", "Auto", "Light", "Dark", "Open-source Licenses"]) expect(texts).toContain(word);
  });

  test("the appearance chosen is kept in the Expo app's key", async () => {
    const c = computer({ name: "settings" }, () => null, { network: { "GET http://desk.test:9877/auth-check": { status: 503 } } });
    const app = await open(HARNESS, c.options);
    await app.press("id:appearance-dark");
    await app.waitUntil(() => app.storage()["mewla:theme_preference"] === "classic-dark");
    await app.press("id:appearance-system");
    await app.waitUntil(() => app.storage()["mewla:theme_preference"] === "system");
  });
});

describe("Model Providers", () => {
  test("lists the agent's connections, and choosing one routes the agent through it", async () => {
    let current = snapshot();
    const c = computer({ name: "settings-providers" }, (type, body) => {
      if (type === "list_providers") return current;
      if (type === "set_provider_connection") {
        current = snapshot({ revision: 4, defaults: { codex: { connection_id: String(body.connection_id) } } });
        return { ...current, persistence_outcome: "applied", persistence_durable: true };
      }
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Work gateway");
    expect(app.texts().join(" ")).toContain("gw.example.com · 1 model");
    expect(app.texts()).toContain("Key required");
    await app.press("id:provider-c1");
    await app.waitUntil(() => c.asked.some((a) => a.type === "set_provider_connection"));
    const sent = c.asked.find((a) => a.type === "set_provider_connection")!.body;
    expect(sent).toEqual({ client: "codex", executor_id: "codex", connection_id: "c1", revision: 3 });
  });

  test("a connection without a key opens its editor instead of being chosen", async () => {
    const c = computer({ name: "settings-providers" }, (type) => (type === "list_providers" ? snapshot() : null));
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Spare");
    await app.press("id:provider-c2");
    await app.waitFor("id:provider-editor");
    expect(c.asked.some((a) => a.type === "set_provider_connection")).toBe(false);
  });

  test("adding a Provider sends its name, endpoint and key in one request", async () => {
    let current = snapshot();
    const c = computer({ name: "settings-provider-editor", client: "codex" }, (type, body) => {
      if (type === "list_providers") return current;
      if (type === "upsert_provider_connection") {
        current = snapshot({ revision: 4, connections: [...snapshot().connections, { id: "c3", name: "Home", preset_id: "custom", clients: ["codex"], credential_ready: true, advanced: true, base_url: "https://home.example.com/v1" }] });
        return { ...current, persistence_outcome: "applied", persistence_durable: true };
      }
      return null;
    });
    const app = await open(HARNESS, { ...c.options, host: { ...c.options.host, params: { page: { name: "settings-providers" } } } });
    await app.waitFor("text:Work gateway");
    await app.press("id:provider-add");
    await app.waitFor("id:provider-name");
    await app.type("id:provider-name", "Work gateway");
    expect(app.texts().join(" ")).toContain("Names must be unique.");
    await app.clear?.("id:provider-name");
    await app.type("id:provider-name", "Home");
    await app.type("id:provider-url", "https://home.example.com/v1");
    await app.type("id:provider-key", "sk-test-123");
    await app.press("id:provider-save");
    await app.waitUntil(() => c.asked.some((a) => a.type === "upsert_provider_connection"));
    const sent = c.asked.find((a) => a.type === "upsert_provider_connection")!.body as Record<string, any>;
    expect(sent.operation).toBe("create");
    expect(sent.credential).toBe("sk-test-123");
    expect(sent.provider_connection).toMatchObject({ name: "Home", client: "codex", base_url: "https://home.example.com/v1", preset_id: "custom" });
    await app.waitFor("text:Home");
  });
});

describe("Telegram", () => {
  test("a token is verified once and never kept on the phone", async () => {
    let status: Record<string, unknown> = { state: "disabled", enabled: false, binding_pending: false };
    const c = computer({ name: "settings-telegram" }, (type, body) => {
      if (type === "telegram_connection_status") return { connection: status };
      if (type === "telegram_connection_configure") {
        expect(body.credential).toBe("123:abc");
        status = { state: "setup_pending", enabled: true, bot_username: "mewla_bot", binding_pending: false };
        return { connection: status };
      }
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:telegram-token");
    await app.type("id:telegram-token", "123:abc");
    await app.press("id:telegram-verify");
    await app.waitFor("text:@mewla_bot");
    expect(app.shows("id:telegram-connect")).toBe(true);
    expect(JSON.stringify(app.storage())).not.toContain("123:abc");
  });
});

describe("Outside agents", () => {
  test("approving a waiting agent tells the computer, and the list follows its answer", async () => {
    let state: Record<string, unknown> = { pending: [{ id: "a1", name: "Claude Code", number: "4821", expires_at: "" }], agents: [] };
    const c = computer({ name: "settings-agents" }, () => null, {
      network: {
        "GET http://desk.test:9877/agents": () => ({ status: 200, body: JSON.stringify(state) }),
        "POST http://desk.test:9877/agents": (request: { body?: string }) => {
          expect(JSON.parse(String(request.body))).toEqual({ action: "approve", id: "a1" });
          state = { pending: [], agents: [{ id: "a1", name: "Claude Code", approved_at: new Date().toISOString() }] };
          return { status: 200, body: JSON.stringify(state) };
        },
      },
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Wants to hand work to Brain. Approve only if its terminal shows 4821.");
    await app.press("id:approve-a1");
    await app.waitFor("text:Not used yet");
  });
});
