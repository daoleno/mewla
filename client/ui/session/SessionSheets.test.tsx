import { describe, expect, open, test } from "nsh/test";

const HARNESS = "./testing/Harness.tsx";

type Answer = (body: Record<string, unknown>) => unknown;

function computer(params: Record<string, unknown>, answers: Record<string, Answer>) {
  const asked: { type: string; body: Record<string, unknown> }[] = [];
  return {
    asked,
    host: {
      params,
      handlers: {
        "app.daemon": ({ type, body }: { type: string; body: Record<string, unknown> }) => {
          asked.push({ type, body });
          const answer = answers[type];
          return answer ? answer(body) : { error: `unexpected ${type}` };
        },
      },
    },
  };
}

const runtime = (patch: Record<string, unknown> = {}) => ({
  session_id: "w1", client: "codex", connection_id: "c1", connection_name: "OpenAI", model_id: "gpt-6",
  reasoning_effort_default: "medium", reasoning_efforts: ["low", "medium", "high"], credential_ready: true, hot_switchable: true, ...patch,
});
const catalog = {
  revision: 1,
  connections: [{ id: "c1", name: "OpenAI", clients: ["codex"], credential_ready: true, advanced: false }],
  defaults: { codex: { connection_id: "c1" } },
  presets: [],
  models: { c1: [
    { id: "gpt-6", available: true, source: "catalog", reasoning_efforts: ["low", "medium", "high"], reasoning_effort_default: "medium" },
    { id: "gpt-6-mini", available: true, source: "catalog" },
  ] },
};
const applied = (body: Record<string, unknown>, patch: Record<string, unknown> = {}) => {
  const asked = body.runtime as { model_id: string; effect?: string };
  return { runtime: runtime({ ...patch, model_id: asked.model_id, reasoning_effort: asked.effect }), persistence_outcome: "applied", persistence_durable: true };
};
// Claude models have no effects: a tap on one switches at once.
const claude = { client: "claude", connection_id: "k1", connection_name: "Anthropic", model_id: "opus", reasoning_effort_default: undefined, reasoning_efforts: undefined };
const claudeCatalog = {
  revision: 1,
  connections: [{ id: "k1", name: "Anthropic", clients: ["claude"], credential_ready: true, advanced: false }],
  defaults: { claude: { connection_id: "k1" } },
  presets: [],
  models: { k1: [{ id: "opus", available: true, source: "catalog" }, { id: "sonnet", available: true, source: "catalog" }] },
};

describe("Model sheet", () => {
  test("lists the models; a model without effects switches and closes the sheet", async () => {
    const c = computer({ sheet: "model" }, { get_thread_runtime: () => ({ runtime: runtime(claude) }), list_providers: () => claudeCatalog, set_thread_runtime: (body) => applied(body, claude) });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:model-row-sonnet");
    expect(app.texts()).toContain("Model & Effect");
    expect(app.count("id:model-row-opus")).toBe(1);
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:closes:1");
    expect(c.asked.find((a) => a.type === "set_thread_runtime")?.body).toEqual({ worker_id: "w1", runtime: { connection_id: "k1", model_id: "sonnet" } });
    expect(app.texts()).toContain("runtime:sonnet:");
  }, 20_000);

  test("a model with effects opens its Effect list; Back returns; an effect switches", async () => {
    const c = computer({ sheet: "model" }, { get_thread_runtime: () => ({ runtime: runtime() }), list_providers: () => catalog, set_thread_runtime: applied });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:model-row-gpt-6");
    await app.press("id:model-row-gpt-6");
    await app.waitFor("id:effect-row-high");
    expect(app.texts()).toContain("Effect");
    expect(app.texts()).toContain("Default");
    await app.press("id:model-back");
    await app.waitFor("id:model-row-gpt-6-mini");
    await app.press("id:model-row-gpt-6");
    await app.press("id:effect-row-high");
    await app.waitFor("text:closes:1");
    expect(c.asked.find((a) => a.type === "set_thread_runtime")?.body.runtime).toEqual({ connection_id: "c1", model_id: "gpt-6", effect: "high" });
  }, 20_000);

  test("a current model no longer offered is flagged", async () => {
    const c = computer({ sheet: "model" }, { get_thread_runtime: () => ({ runtime: runtime({ model_id: "gpt-5-old" }) }), list_providers: () => catalog });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:model-unavailable-current");
    expect(app.texts()).toContain("Current runtime is no longer selectable.");
    expect(app.texts()).toContain("gpt-5-old");
  }, 20_000);

  test("a load failure says why and Retry reads again", async () => {
    let fail = true;
    const c = computer({ sheet: "model" }, {
      get_thread_runtime: () => (fail ? { error: "session route not found", code: "route_binding_not_found" } : { runtime: runtime() }),
      list_providers: () => catalog,
    });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:model-problem");
    expect(app.texts()).toContain("Could not load models");
    expect(app.texts()).toContain("Session Model selection not found");
    fail = false;
    await app.press("id:model-retry");
    await app.waitFor("id:model-row-gpt-6-mini");
  }, 20_000);

  test("a busy Session keeps the sheet open and lets the person try again", async () => {
    let busy = true;
    const c = computer({ sheet: "model" }, {
      get_thread_runtime: () => ({ runtime: runtime(claude) }),
      list_providers: () => claudeCatalog,
      set_thread_runtime: (body) => (busy ? { error: "busy", code: "route_binding_busy" } : applied(body, claude)),
    });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:model-row-sonnet");
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:Could not switch model");
    busy = false;
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:closes:1");
  }, 20_000);

  test("after a switch of unknown outcome, the next one waits for Retry", async () => {
    let broken = true;
    const c = computer({ sheet: "model" }, {
      get_thread_runtime: () => ({ runtime: runtime(claude) }),
      list_providers: () => claudeCatalog,
      set_thread_runtime: (body) => (broken ? { error: "connection reset" } : applied(body, claude)),
    });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:model-row-sonnet");
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:connection reset");
    broken = false;
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:Refresh required before switching runtime.");
    expect(c.asked.filter((a) => a.type === "set_thread_runtime").length).toBe(1);
    await app.press("id:model-retry");
    await app.waitFor("id:model-row-sonnet");
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:closes:1");
  }, 20_000);
});

const statusPayload = {
  thread: { id: "th-1", model: "gpt-6", effort: "high", cwd: "/repo" },
  account: { kind: "chatgpt", plan: "pro" },
  limits: [{ id: "codex", windows: [{ used_percent: 25, window_minutes: 300 }] }],
};

describe("Codex status sheet", () => {
  test("shows the meters and rows, and Refresh reads again", async () => {
    const c = computer({ sheet: "status" }, { codex_status: () => ({ status: statusPayload }) });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:status-meter");
    const texts = app.texts();
    expect(texts).toContain("5 hours");
    expect(texts).toContain("75% left");
    expect(texts).toContain("gpt-6 (high)");
    expect(texts).toContain("ChatGPT");
    expect(c.asked[0]).toEqual({ type: "codex_status", body: { target_id: "w1" } });
    await app.press("id:status-refresh");
    await app.waitFor("id:status-meter");
    expect(c.asked.filter((a) => a.type === "codex_status").length).toBe(2);
  }, 20_000);

  test("a chat Mewla did not start has no status: no Retry, the terminal instead", async () => {
    const c = computer({ sheet: "status" }, { codex_status: () => ({ error: "no app-server", code: "codex_status_unavailable" }) });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:status-failed");
    expect(app.texts()).toContain("No status for this chat");
    expect(app.count("id:status-retry")).toBe(0);
    await app.press("id:status-terminal");
    await app.waitFor("text:terminal");
  }, 20_000);

  test("another failure offers Retry", async () => {
    let fail = true;
    const c = computer({ sheet: "status" }, { codex_status: () => (fail ? { error: "app-server crashed" } : { status: statusPayload }) });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("text:Couldn't load status");
    expect(app.texts()).toContain("app-server crashed");
    fail = false;
    await app.press("id:status-retry");
    await app.waitFor("id:status-meter");
  }, 20_000);
});

describe("Codex skills sheet", () => {
  const skills = { skills: [{ name: "deploy", path: "/s/deploy", description: "Ship the site" }, { name: "lint", path: "/s/lint", scope: "repo", enabled: false }] };

  test("lists the folder's skills; search filters; a tap picks and closes", async () => {
    const c = computer({ sheet: "skills", cwd: "/repo" }, { codex_skills: () => skills });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:skill-deploy");
    expect(c.asked[0]).toEqual({ type: "codex_skills", body: { cwd: "/repo" } });
    expect(app.count("id:skill-lint")).toBe(1);
    await app.type("id:skills-search", "ship");
    await app.waitFor("id:skill-deploy");
    expect(app.count("id:skill-lint")).toBe(0);
    await app.press("id:skills-search-clear");
    await app.type("id:skills-search", "zzz");
    await app.waitFor("text:No matching skills");
    await app.press("id:skills-search-clear");
    await app.waitFor("id:skill-lint");
    await app.press("id:skill-lint");
    await app.waitFor("text:picked:lint");
    expect(app.texts()).toContain("closes:1");
  }, 20_000);

  test("a failure says so", async () => {
    const app = await open(HARNESS, computer({ sheet: "skills" }, { codex_skills: () => ({ error: "codex not installed" }) }));
    await app.press("id:harness-open");
    await app.waitFor("id:skills-failed");
    expect(app.texts()).toContain("codex not installed");
  }, 20_000);
});

const slashCommands = {
  commands: [
    { value: "/status", name: "status", description: "Show status", category: "session", chat_supported: true },
    { value: "/rename", name: "rename", description: "Rename the thread", category: "session", chat_supported: true },
    { value: "/fork", name: "fork", description: "Fork the thread", category: "navigation", execution: "terminal-required", chat_supported: true },
    { value: "/init", name: "init", description: "Create AGENTS.md", category: "tools", chat_supported: true },
  ],
};

describe("slash menu", () => {
  test("typing /x shows the matching commands; a space hides them", async () => {
    const app = await open(HARNESS, computer({ sheet: "slash" }, { codex_slash_commands: () => slashCommands }));
    await app.waitFor("text:commands:4");
    expect(app.count("id:slash-menu")).toBe(0);
    await app.type("id:harness-text", "/re");
    await app.waitFor("id:slash-rename");
    expect(app.count("id:slash-status")).toBe(0);
    await app.type("id:harness-text", "name x");
    await app.waitFor("text:commands:4");
    expect(app.count("id:slash-menu")).toBe(0);
  }, 20_000);

  test("+ shows Attach File and the commands by group; a pick routes", async () => {
    const app = await open(HARNESS, computer({ sheet: "slash" }, { codex_slash_commands: () => slashCommands }));
    await app.waitFor("text:commands:4");
    await app.press("id:composer-plus");
    await app.waitFor("id:slash-menu");
    expect(app.texts()).toContain("Add a file to this message");
    expect(app.count("id:slash-group-session")).toBe(1);
    expect(app.count("id:slash-group-navigation")).toBe(1);
    await expect(app.press("id:attach-file")).rejects.toMatchObject({ code: "ERR_NSH_TEST_DISABLED" });
    await app.press("id:slash-fork");
    await app.waitFor('text:{"kind":"send","text":"/fork"}');
    expect(app.count("id:slash-menu")).toBe(0);
    await app.press("id:composer-plus");
    await app.press("id:slash-status");
    await app.waitFor('text:{"kind":"status"}');
  }, 20_000);

  test("send routes /rename without arguments to a prefill, and an unavailable command to its alert", async () => {
    const app = await open(HARNESS, computer({ sheet: "slash" }, { codex_slash_commands: () => slashCommands }));
    await app.waitFor("text:commands:4");
    await app.type("id:harness-text", "/rename");
    await app.press("id:harness-send");
    await app.waitFor('text:{"kind":"prefill","text":"/rename "}');
    app.onDialog(() => "Open Terminal");
    await app.press("id:harness-clear");
    await app.type("id:harness-text", "/init");
    await app.press("id:harness-send");
    await app.waitFor('text:{"kind":"prefill","text":"/init","alert":"/init is not available"} pressed:open_terminal');
    expect(app.dialogs().at(-1)).toMatchObject({ title: "/init is not available" });
  }, 20_000);

  test("for Claude nothing is a command: no list, the text goes as typed", async () => {
    const app = await open(HARNESS, computer({ sheet: "slash", agentKind: "claude" }, { codex_slash_commands: () => slashCommands }));
    await app.waitFor("text:commands:4");
    await app.type("id:harness-text", "/st");
    await app.press("id:harness-send");
    await app.waitFor('text:{"kind":"send","text":"/st"}');
    expect(app.count("id:slash-menu")).toBe(0);
  }, 20_000);

  test("an unloaded list says the commands are unavailable", async () => {
    const app = await open(HARNESS, computer({ sheet: "slash" }, { codex_slash_commands: () => ({ error: "no codex" }) }));
    await app.type("id:harness-text", "/st");
    await app.waitFor("id:slash-empty");
    expect(app.texts()).toContain("Codex commands unavailable");
  }, 20_000);
});
