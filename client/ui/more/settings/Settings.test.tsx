import { describe, expect, open, test } from "nsh/test";
import { pressWhenEnabled } from "../../testing/fixtures";

const HARNESS = "../testing/Harness.tsx";
const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

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
  gateway: { running: true },
  ...overrides,
});

describe("Settings", () => {
  test("lists the agents, Labs, Appearance, Pet and About, and no Channels", async () => {
    const c = computer({ name: "settings" }, () => null, {
      network: { "GET http://desk.test:9877/auth-check": { status: 503 } },
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Model Providers");
    const texts = app.texts();
    for (const word of ["Model Providers", "Outside agents", "Friends", "Auto", "Light", "Dark", "Open-source Licenses"]) expect(texts).toContain(word);
    expect(texts).not.toContain("Channels");
    expect(texts).not.toContain("Telegram");
    expect(c.asked.filter((a) => a.type.startsWith("telegram_"))).toEqual([]);
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

describe("Pet", () => {
  test("the pet the Expo app chose stays chosen, and a new choice applies at once and is kept in the same key", async () => {
    const c = computer({ name: "settings-pet" }, () => null, { storage: { "mewla:pet_preference": "p13" } });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Bit at work");
    await app.press("id:pet-p14");
    await app.waitFor("text:Doodle at work");
    await app.waitFor("text:Doodle moved in");
    await app.waitUntil(() => app.storage()["mewla:pet_preference"] === "p14");
  });

  test("a pet that was removed falls back to Dango", async () => {
    const c = computer({ name: "settings-pet" }, () => null, { storage: { "mewla:pet_preference": "p03" } });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Dango at work");
  });
});

describe("Model Providers", () => {
  test("each agent shows what it uses; its page chooses, and choosing routes the agent through it", async () => {
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
    expect(app.texts()).toContain("Official login");
    await app.press("id:provider-pick-codex");
    await app.waitFor("id:provider-pick-codex-c1");
    await app.press("id:provider-pick-codex-c1");
    await app.waitUntil(() => c.asked.some((a) => a.type === "set_provider_connection"));
    const sent = c.asked.find((a) => a.type === "set_provider_connection")!.body;
    expect(sent).toEqual({ client: "codex", executor_id: "codex", connection_id: "c1", revision: 3 });
  });

  test("a Provider opens one page with its key, its test and its models, and asks for its models again", async () => {
    const c = computer({ name: "settings-providers" }, (type) => {
      if (type === "list_providers") return snapshot();
      if (type === "discover_provider_models") return { reply: "provider_models", connection_id: "c1", models: [{ id: "gpt-6", display_name: "GPT 6", available: true, source: "upstream" }, { id: "gpt-6.2", available: true, source: "upstream" }] };
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-c1");
    await app.press("id:provider-c1");
    await app.waitFor("id:provider-detail");
    await app.waitFor("id:model-gpt-6.2");
    expect(app.shows("id:provider-key-row")).toBe(true);
    expect(app.shows("id:provider-test-row")).toBe(true);
    expect(app.shows("id:provider-delete")).toBe(true);
    expect(c.asked.some((a) => a.type === "discover_provider_models")).toBe(true);
  });

  test("a Provider names the API it speaks, and its Fallback picks one that can take its requests", async () => {
    const pair = (fallback = "") => snapshot({
      revision: fallback ? 5 : 4,
      connections: [
        { ...snapshot().connections[0], upstream_api: "openai_responses" },
        { ...snapshot().connections[1], upstream_api: "openai_responses" },
        { id: "c3", name: "Local GLM", clients: ["codex"], credential_ready: true, advanced: true, base_url: "https://glm.example.com/v1", upstream_api: "openai_chat", fallback_connection_id: fallback || undefined },
      ],
    });
    let current = pair();
    const c = computer({ name: "settings-providers" }, (type, body) => {
      if (type === "list_providers") return current;
      if (type === "discover_provider_models") return { reply: "provider_models", connection_id: String(body.connection_id), models: [] };
      if (type === "set_provider_fallback") {
        current = pair(String(body.fallback_connection_id));
        return { ...current, persistence_outcome: "applied", persistence_durable: true };
      }
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-c3");
    await app.press("id:provider-c3");
    await app.waitFor("id:provider-detail");
    expect(app.texts()).toContain("Chat Completions");
    expect(app.texts()).toContain("None");
    await app.press("id:provider-fallback-pick");
    await app.waitFor("id:provider-fallback-c1");
    expect(app.shows("id:provider-fallback-c3")).toBe(false);
    await app.press("id:provider-fallback-c1");
    await app.waitUntil(() => c.asked.some((a) => a.type === "set_provider_fallback"));
    expect(c.asked.find((a) => a.type === "set_provider_fallback")!.body).toEqual({ connection_id: "c3", fallback_connection_id: "c1" });
    await app.waitUntil(() => app.texts().includes("Work gateway") && !app.texts().includes("None"));
  });

  test("a Provider shows what its requests used and its last failures", async () => {
    const now = Date.now();
    const at = (minutesAgo: number) => new Date(now - minutesAgo * 60_000).toISOString();
    const d = new Date(now);
    const today = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
    const requests = [
      { at: at(2), agent: "claude", model: "glm-5", input_tokens: 1200, cache_read_tokens: 48000, output_tokens: 350, cost_usd: 0.0123, priced: true, ms: 2100 },
      { at: at(9), agent: "codex", model: "kimi-k2", input_tokens: 900, output_tokens: 40, ms: 800 },
      ...Array.from({ length: 5 }, (_, i) => ({ at: at(20 + i), agent: "claude", model: "glm-5", input_tokens: 10, output_tokens: 1, cost_usd: 0.5, priced: true, ms: 100 })),
    ];
    const c = computer({ name: "settings-providers" }, (type, body) => {
      if (type === "list_providers") return snapshot();
      if (type === "discover_provider_models") return { reply: "provider_models", connection_id: "c1", models: [] };
      if (type === "provider_usage") {
        return {
          reply: "provider_usage", connection_id: String(body.connection_id),
          usage: {
            requests,
            errors: [{ at: at(5), agent: "codex", model: "kimi-k2", status: 429, message: "Rate limit reached for requests" }],
            days: { [today]: { requests: 7, errors: 1, input_tokens: 2150, cache_read_tokens: 48000, output_tokens: 395, cost_usd: 2.5123, unpriced: 1 } },
          },
        };
      }
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-c1");
    await app.press("id:provider-c1");
    await app.waitFor("id:provider-usage");
    expect(c.asked.find((a) => a.type === "provider_usage")!.body).toEqual({ connection_id: "c1" });
    const texts = app.texts();
    // One request had no price: the cost is at least what is known.
    expect(texts).toContain("$2.51+");
    expect(texts).toContain("50.5K");
    expect(texts).toContain("1 failed");
    expect(texts).toContain("$0.0123");
    expect(texts).toContain("1.2K in · 48K cached · 350 out");
    expect(texts).toContain("900 in · 40 out");
    expect(texts).toContain("2m");
    expect(texts).toContain("Rate limit reached for requests");
    expect(texts).toContain("429 · Codex · kimi-k2 · 5m");
    // Five rows, then the rest on request.
    expect(app.shows("id:provider-usage-request-5")).toBe(false);
    await app.press("id:provider-usage-more");
    await app.waitFor("id:provider-usage-request-6");
  });

  test("a Provider of its agent's own API is offered no fallback of that API", async () => {
    const c = computer({ name: "settings-providers" }, (type) => {
      if (type === "list_providers") return snapshot({ connections: snapshot().connections.map((x) => ({ ...x, upstream_api: "openai_responses" })) });
      if (type === "discover_provider_models") return { reply: "provider_models", connection_id: "c1", models: [] };
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-c1");
    await app.press("id:provider-c1");
    await app.waitFor("id:provider-detail");
    expect(app.texts()).toContain("Responses");
    expect(app.shows("id:provider-fallback")).toBe(false);
  });

  const PRESETS = [
    { id: "openrouter", label: "OpenRouter", clients: ["codex", "claude"] },
    { id: "kimi", label: "Kimi", clients: ["codex", "claude"] },
    { id: "anthropic", label: "Anthropic", clients: ["claude"] },
    { id: "ollama", label: "Ollama", clients: ["codex", "claude"], local: true, base_url: "http://localhost:11434" },
    { id: "local", label: "Local server", clients: ["codex", "claude"], local: true },
    { id: "custom", label: "Custom Gateway", clients: ["codex", "claude"], advanced: true },
  ];

  test("a vendor preset asks only for the key: its address and API are the daemon's", async () => {
    const c = computer({ name: "settings-providers" }, (type) => {
      if (type === "list_providers") return snapshot({ presets: PRESETS });
      if (type === "detect_local_providers") return { reply: "local_providers", found: [] };
      if (type === "upsert_provider_connection") return { ...snapshot({ presets: PRESETS }), persistence_outcome: "applied", persistence_durable: true };
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-add");
    await app.press("id:provider-add");
    await app.waitFor("id:preset-kimi");
    // Codex is the default here: Anthropic serves Claude Code only.
    expect(app.shows("id:preset-anthropic")).toBe(false);
    expect(app.shows("id:preset-ollama")).toBe(true);
    await app.press("id:preset-kimi");
    await app.waitFor("id:provider-key");
    expect(app.shows("id:provider-url")).toBe(false);
    expect(app.texts()).toContain("Add Kimi");
    await app.type("id:provider-key", "sk-fixture-not-real");
    await pressWhenEnabled(app, "id:provider-save");
    await app.waitUntil(() => c.asked.some((a) => a.type === "upsert_provider_connection"));
    const sent = c.asked.find((a) => a.type === "upsert_provider_connection")!.body;
    expect(sent.provider_connection).toEqual({ name: "Kimi", preset_id: "kimi", client: "codex", advanced: false });
    expect(sent.credential).toBe("sk-fixture-not-real");
  });

  test("a model server found running is offered first, and is added without a key", async () => {
    const c = computer({ name: "settings-providers" }, (type) => {
      if (type === "list_providers") return snapshot({ presets: PRESETS });
      if (type === "detect_local_providers") return { reply: "local_providers", found: [{ preset_id: "ollama", label: "Ollama", base_url: "http://localhost:11434", models: 2 }] };
      if (type === "upsert_provider_connection") return { ...snapshot({ presets: PRESETS }), persistence_outcome: "applied", persistence_durable: true };
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-add");
    await app.press("id:provider-add");
    await app.waitFor("text:On this computer");
    expect(app.texts()).toContain("localhost:11434 · 2 models");
    await app.press("id:local-ollama-localhost:11434");
    await app.waitFor("id:provider-url");
    expect(app.shows("id:provider-key")).toBe(false);
    await pressWhenEnabled(app, "id:provider-save");
    await app.waitUntil(() => c.asked.some((a) => a.type === "upsert_provider_connection"));
    const sent = c.asked.find((a) => a.type === "upsert_provider_connection")!.body;
    expect(sent.provider_connection).toEqual({ name: "Ollama", preset_id: "ollama", client: "codex", base_url: "http://localhost:11434", advanced: true });
    expect(sent.credential).toBeUndefined();
  });

  test("a local server's page shows its address, not a key", async () => {
    const c = computer({ name: "settings-providers" }, (type) => {
      if (type === "list_providers") return snapshot({ presets: PRESETS, connections: [{ id: "c9", name: "Ollama", preset_id: "ollama", clients: ["claude"], credential_ready: true, advanced: true, base_url: "http://localhost:11434", upstream_api: "openai_chat" }], models: {} });
      if (type === "discover_provider_models") return { reply: "provider_models", connection_id: "c9", models: [] };
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-c9");
    await app.press("id:provider-c9");
    await app.waitFor("id:provider-detail");
    expect(app.shows("id:provider-key-row")).toBe(false);
    expect(app.texts()).toContain("localhost:11434");
    expect(app.texts()).toContain("Chat Completions");
  });

  test("each agent's chip has a dot for its state: none on its own login, green when ready", async () => {
    const c = computer({ name: "settings-providers" }, (type) => (type === "list_providers" ? snapshot({ defaults: { codex: { connection_id: "c1" }, claude: { connection_id: "" } } }) : null));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-dot-codex-ok");
    expect(app.texts()).toContain("Work gateway");
    expect(app.shows("id:provider-dot-codex-ok")).toBe(true);
    expect(app.shows("id:provider-dot-claude-off")).toBe(true);
  });

  test("on a wide pane the agents and the Providers are the list, the shown Provider's page scrolls beside it, and a row shows its Provider there", async () => {
    // A long model list (17, as on the owner's Mac) once pushed the Providers below it, out of the window.
    const models = Array.from({ length: 17 }, (_, i) => ({ id: `m-${i}`, display_name: `Model ${i}`, available: true, source: "upstream" }));
    const c = computer({ name: "settings-providers" }, (type) => {
      if (type === "list_providers") return snapshot({ defaults: { codex: { connection_id: "c1" } }, models: { c1: models }, connections: [...snapshot().connections, { id: "c3", name: "Home", clients: ["claude"], credential_ready: true, advanced: true, base_url: "https://home.example.com/v1" }] });
      if (type === "discover_provider_models") return { reply: "provider_models", connection_id: "c1", models: [] };
      return null;
    }, { width: 1280, height: 800 });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-panel");
    await app.waitFor("id:model-m-16");
    await app.waitFor("id:provider-c1-shown");
    const list = app.layout("id:provider-c3")!;
    const detail = app.layout("id:providers-shown")!;
    expect(list.x + list.width).toBeLessThanOrEqual(detail.x);
    expect(app.layout("id:provider-pick-codex")!.x).toBeLessThan(detail.x);
    expect(list.y + list.height).toBeLessThanOrEqual(800);
    await app.press("id:provider-c3");
    await app.waitFor("id:provider-c3-shown");
    await app.waitUntil(() => app.texts().includes("home.example.com"));
    expect(app.shows("id:provider-detail")).toBe(false);
    expect(app.texts()).toContain("Home");
  });

  test("a connection without a key opens its editor instead of being chosen", async () => {
    const c = computer({ name: "settings-providers" }, (type) => (type === "list_providers" ? snapshot() : null));
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Spare");
    await app.press("id:provider-pick-codex");
    await app.waitFor("id:provider-pick-codex-c2");
    await app.press("id:provider-pick-codex-c2");
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
    await app.press("id:preset-custom");
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

describe("Provider editor", () => {
  // FINAL-VERIFY 6387f88f m4 settings [182].
  test("Test connection says why it failed, and a changed field takes an old notice away", async () => {
    const c = computer({ name: "settings-provider-editor", client: "codex" }, (type) => {
      if (type === "list_providers") return snapshot();
      if (type === "test_provider_connection") return { error: 'Get "http://127.0.0.1:9/models": dial tcp 127.0.0.1:9: connect: connection refused', code: "provider_connection_test_failed" };
      return null;
    });
    const app = await open(HARNESS, { ...c.options, host: { ...c.options.host, params: { page: { name: "settings-providers" } } } });
    await app.waitFor("text:Work gateway");
    await app.press("id:provider-add");
    await app.press("id:preset-custom");
    await app.waitFor("id:provider-name");
    await app.type("id:provider-name", "Fake verify");
    await app.type("id:provider-url", "ftp://127.0.0.1:9/v1");
    await app.type("id:provider-key", "sk-fake-1");
    await app.press("id:provider-save");
    await app.waitFor("text:Base URL must use HTTP or HTTPS.");
    await app.clear?.("id:provider-url");
    await app.type("id:provider-url", "http://127.0.0.1:9/v1");
    expect(app.shows("text:Base URL must use HTTP or HTTPS.")).toBe(false);
    await app.type("id:provider-key", "sk-fake-1"); // a Save takes the key out of its field
    await app.press("id:provider-test");
    await app.waitUntil(() => app.texts().some((t) => t.endsWith("connect: connection refused")));
    expect(app.texts().join(" ")).not.toContain("Providers are not available");
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

describe("iPhone use", () => {
  const mac = { enabled: true, state: "ready", summary: "Agents on Studio can use your iPhone." };

  test("Labs shows iPhone use only where the computer offers it, with its state", async () => {
    const c = computer({ name: "settings" }, () => null, {
      network: { "GET http://desk.test:9877/auth-check": { status: 503 }, "GET http://desk.test:9877/iphone": json({ ...mac, active: { worker: "w1", name: "Codex", since: "" } }) },
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:settings-iphone");
    expect(app.texts()).toContain("In use");

    const linux = computer({ name: "settings" }, () => null, {
      network: { "GET http://desk.test:9877/auth-check": { status: 503 }, "GET http://desk.test:9877/iphone": json({ enabled: false, state: "unsupported", summary: "" }) },
    });
    const other = await open(HARNESS, linux.options);
    await other.waitFor("text:Labs");
    await other.waitUntil(() => other.requests().some((r) => r.url.startsWith("http://desk.test:9877/iphone")));
    expect(other.shows("id:settings-iphone")).toBe(false);
  });

  test("while an agent uses the iPhone, Done tells it the person finished and Stop stops it", async () => {
    const sent: unknown[] = [];
    const c = computer({ name: "settings-iphone" }, () => null, {
      network: {
        "GET http://desk.test:9877/iphone?picture=1": json({ ...mac, active: { worker: "w1", name: "Codex", since: "" } }),
        "POST http://desk.test:9877/iphone": (request: { body?: string }) => {
          sent.push(JSON.parse(String(request.body)));
          return json({ ...mac, active: { worker: "w1", name: "Codex", since: "" } });
        },
      },
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Codex is using your iPhone");
    expect(app.texts()).toContain("Agents on Studio can use your iPhone.");
    await app.press("label:Done: tell the agent you finished");
    await app.waitUntil(() => sent.length === 1);
    // Done holds both buttons until the computer answers it.
    await pressWhenEnabled(app, "label:Stop Codex");
    await app.waitUntil(() => sent.length === 2);
    expect(sent).toEqual([{ handoff_done: true }, { stop_agent: true }]);
  });

  test("a computer without iPhone use says so, and the switch stays off", async () => {
    const c = computer({ name: "settings-iphone" }, () => null, { network: { "GET http://desk.test:9877/iphone?picture=1": { status: 404, body: "not found" } } });
    const app = await open(HARNESS, c.options);
    await app.waitUntil(() => app.texts().some((t) => t.startsWith("This computer doesn't offer iPhone use (")));
    expect(app.texts().some((t) => t.includes("no response is listed"))).toBe(false);
    expect(app.enabled("id:iphone-switch")).toBe(false);
  });
});

describe("Virtual screen and Phone use", () => {
  test("Virtual screen is a row of Agents, not of Labs, and Labs keeps Friends", async () => {
    const c = computer({ name: "settings" }, () => null, {
      network: { "GET http://desk.test:9877/auth-check": { status: 503 }, "POST http://desk.test:9877/browser": json({ virtual_screen: false }) },
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:settings-virtual-screen");
    const texts = app.texts();
    const at = (word: string) => texts.indexOf(word);
    expect(at("Agents")).toBeLessThan(at("Virtual screen"));
    expect(at("Virtual screen")).toBeLessThan(at("Labs"));
    expect(at("Labs")).toBeLessThan(at("Friends"));
  });

  test("Virtual screen shows the computer's switch and opens its page, which turns it on", async () => {
    let on = false;
    const c = computer({ name: "settings" }, () => null, {
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "POST http://desk.test:9877/browser": (request: { body?: string }) => {
          const body = JSON.parse(String(request.body)) as { enabled?: boolean };
          if (typeof body.enabled === "boolean") on = body.enabled;
          return json({ virtual_screen: on });
        },
      },
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:settings-virtual-screen");
    await app.press("id:settings-virtual-screen");
    await app.waitFor("id:virtual-screen-page");
    // `enabled` throws while no view matches: the switch first, then that it takes a press.
    await app.waitFor("id:virtual-screen");
    await pressWhenEnabled(app, "id:virtual-screen");
    await app.waitUntil(() => on);
    await app.back();
    await app.waitUntil(() => app.texts().includes("On"));
  });

  test("Share anonymous usage counts is the computer's switch: off tells it, on again tells it", async () => {
    let enabled = true;
    const posted: Array<Record<string, unknown>> = [];
    const c = computer({ name: "settings" }, () => null, {
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "GET http://desk.test:9877/usage": () => json({ enabled, locked: "" }),
        "POST http://desk.test:9877/usage": (request: { body?: string }) => {
          const body = JSON.parse(String(request.body)) as Record<string, unknown>;
          posted.push(body);
          if (body.action === "set") enabled = body.enabled === true;
          return json({ enabled, locked: "" });
        },
      },
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:usage-counts");
    expect(app.texts()).toContain("Privacy");
    await pressWhenEnabled(app, "id:usage-counts");
    await app.waitUntil(() => !enabled);
    expect(posted.filter((b) => b.action === "set")).toEqual([{ action: "set", enabled: false }]);
    await pressWhenEnabled(app, "id:usage-counts");
    await app.waitUntil(() => enabled);
  });

  test("usage counts locked off on the computer show why and take no press; an older computer has no row", async () => {
    const locked = computer({ name: "settings" }, () => null, {
      network: { "GET http://desk.test:9877/auth-check": { status: 503 }, "GET http://desk.test:9877/usage": json({ enabled: false, locked: "environment" }) },
    });
    let app = await open(HARNESS, locked.options);
    await app.waitFor("text:Off on this computer by MEWLA_TELEMETRY or DO_NOT_TRACK");
    expect(app.enabled("id:usage-counts")).toBe(false);

    const older = computer({ name: "settings" }, () => null, {
      network: { "GET http://desk.test:9877/auth-check": { status: 503 }, "GET http://desk.test:9877/usage": { status: 404, body: "404 page not found" } },
    });
    app = await open(HARNESS, older.options);
    await app.waitUntil(() => app.requests().some((r) => r.url === "http://desk.test:9877/usage"));
    await app.waitFor("text:Outside agents");
    expect(app.shows("id:usage-counts")).toBe(false);
  });

  test("a computer without a virtual screen has no Virtual screen row", async () => {
    const c = computer({ name: "settings" }, () => null, {
      network: { "GET http://desk.test:9877/auth-check": { status: 503 }, "POST http://desk.test:9877/browser": json({ virtual_screen: false, virtual_screen_unsupported: true }) },
    });
    const app = await open(HARNESS, c.options);
    await app.waitUntil(() => app.requests().some((r) => r.url === "http://desk.test:9877/browser"));
    await app.waitFor("text:Outside agents");
    expect(app.shows("id:settings-virtual-screen")).toBe(false);
  });

  test("on the Android GitHub build, Phone use is there with its state and opens its page; the Play build has no row", async () => {
    const status = { supported: true, sdk: 35, enabled: false, link: false, link_state: "offline", developer_options: false, wireless_debugging: false, wifi: true, paired: false, helper: false, problem: null, accessibility: false, restricted: false, self_healing: true, guide: null, notifications: true, state: "off", w: 0, h: 0, display_info: null, agent: null, handoff: null, drawn: false, opening: null, launch_failed: null };
    const c = computer({ name: "settings" }, () => null, { platform: "android", network: { "GET http://desk.test:9877/auth-check": { status: 503 } } });
    const handlers = { ...c.options.host.handlers, "phone.status": () => status, "phone.follow": () => null };
    const app = await open(HARNESS, { ...c.options, host: { ...c.options.host, handlers } });
    await app.waitFor("id:settings-phone");
    expect(app.texts()).toContain("Off");
    // A row of Agents, above Labs.
    expect(app.texts().indexOf("Phone use")).toBeLessThan(app.texts().indexOf("Labs"));
    await app.press("id:settings-phone");
    await app.waitFor("id:phone-page");

    const play = computer({ name: "settings" }, () => null, { platform: "android", network: { "GET http://desk.test:9877/auth-check": { status: 503 } } });
    const other = await open(HARNESS, play.options);
    await other.waitFor("text:Outside agents");
    expect(other.shows("id:settings-phone")).toBe(false);
  });

  test("on iOS there is no Phone use row", async () => {
    const c = computer({ name: "settings" }, () => null, { platform: "ios", network: { "GET http://desk.test:9877/auth-check": { status: 503 } } });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Outside agents");
    expect(app.shows("id:settings-phone")).toBe(false);
  });
});

describe("In a browser", () => {
  const mac = { enabled: true, state: "ready", summary: "Agents on Studio can use your iPhone." };
  const alertCalls = ["calendar.alertsSetStrong", "calendar.alertsCapabilities", "calendar.alertsOpenSettings", "phone.status", "phone.follow"];

  test("Virtual screen is offered; no Labs (Friends, iPhone use), Phone use or Calendar section, and nothing asks the device", async () => {
    const c = computer({ name: "settings" }, () => null, {
      platform: "web",
      network: {
        "GET http://desk.test:9877/auth-check": { status: 503 },
        "GET http://desk.test:9877/iphone": json({ ...mac, active: { worker: "w1", name: "Codex", since: "" } }),
        "POST http://desk.test:9877/browser": json({ virtual_screen: true }),
      },
    });
    const handlers: Record<string, unknown> = { ...c.options.host.handlers };
    for (const name of alertCalls) handlers[name] = () => null;
    const app = await open(HARNESS, { ...c.options, host: { ...c.options.host, handlers } });
    await app.waitFor("id:settings-virtual-screen");
    for (const id of ["settings-friends", "settings-phone", "settings-iphone", "strong-alerts", "full-screen-alerts"]) expect(app.shows(`id:${id}`)).toBe(false);
    expect(app.texts()).not.toContain("Calendar");
    expect(app.texts()).not.toContain("Labs");
    for (const word of ["Appearance", "Pet", "About", "Privacy Policy", "Support", "Open-source Licenses"]) expect(app.texts()).toContain(word);
    await app.wait(200);
    expect(app.requests().some((r) => r.url.startsWith("http://desk.test:9877/iphone"))).toBe(false);
    expect(app.host.calls().filter((call) => alertCalls.includes(call.name))).toEqual([]);
  });
});

describe("Open-source Licenses", () => {
  test("lists what the app ships and opens a license's text", async () => {
    const c = computer({ name: "settings-licenses" }, () => null);
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Bundled code");
    expect(app.texts()).toContain("noVNC");
    expect(app.texts()).toContain("1.7.0 · MPL-2.0");
    await app.press("text:noVNC");
    await app.waitFor("id:license-text");
    expect(app.texts().some((t) => t.includes("Mozilla Public License"))).toBe(true);
  });

  test("an unknown license says it is not found", async () => {
    const c = computer({ name: "settings-license", id: "9/9" }, () => null);
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:License not found");
  });
});
