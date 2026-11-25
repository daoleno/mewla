import { describe, expect, open, test } from "nsh/test";
import { answering } from "../testing/fixtures";

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
    expect(app.texts()).toContain("Model & Effort");
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
    expect(app.texts()).toContain("Effort");
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
      set_thread_runtime: (body) => (broken ? { error: "no answer from the computer", code: "providers_timeout" } : applied(body, claude)),
    });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:model-row-sonnet");
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:no answer from the computer");
    broken = false;
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:Refresh required before switching runtime.");
    expect(c.asked.filter((a) => a.type === "set_thread_runtime").length).toBe(1);
    await app.press("id:model-retry");
    await app.waitFor("id:model-row-sonnet");
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:closes:1");
  }, 20_000);

  test("a pick held for the end of the turn says so, and the open sheet shows the switch once Claude made it", async () => {
    // 11-25: the owner saw only an hourglass on Opus through a long turn; it
    // stayed after Claude had switched. Labels carry the version Claude names.
    const own = {
      client: "claude", connection_id: "claude", connection_name: "Claude Code", model_id: "sonnet", reasoning_effort_default: undefined, reasoning_efforts: undefined,
      native_models: [
        { id: "default", display_name: "Default · Opus 5.5", available: true, source: "claude_native" },
        { id: "opus", display_name: "Opus 5.5", available: true, source: "claude_native" },
        { id: "sonnet", display_name: "Sonnet 5.5", available: true, source: "claude_native" },
      ],
    };
    let now: Record<string, unknown> = own;
    const c = computer({ sheet: "model" }, {
      get_thread_runtime: () => ({ runtime: runtime(now) }),
      set_thread_runtime: () => {
        now = { ...own, pending_model_id: "opus" };
        return { runtime: runtime(now), persistence_outcome: "applied", persistence_durable: true };
      },
    });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:model-current-sonnet");
    for (const label of ["Default · Opus 5.5", "Opus 5.5", "Sonnet 5.5"]) expect(app.texts()).toContain(label);
    await app.press("id:model-row-opus");
    await app.waitFor("id:effect-row-default");
    expect(app.texts()).toContain("Opus 5.5");
    await app.press("id:effect-row-default");
    await app.waitFor("text:Opus 5.5 after this turn");
    expect(app.count("id:model-pending-opus")).toBe(1);
    expect(app.texts()).not.toContain("closes:1");
    // The turn ends and Claude switches: the sheet, still open, follows.
    now = { ...own, model_id: "opus" };
    await app.waitFor("id:model-current-opus", { timeout: 6_000 });
    expect(app.count("id:model-pending-opus")).toBe(0);
    expect(app.count("id:model-switch-note")).toBe(0);
  }, 20_000);

  test("a held pick Claude did not take says why", async () => {
    const own = {
      client: "claude", connection_id: "claude", connection_name: "Claude Code", model_id: "sonnet", reasoning_effort_default: undefined, reasoning_efforts: undefined,
      native_models: [
        { id: "opus", display_name: "Opus 5.5", available: true, source: "claude_native" },
        { id: "sonnet", display_name: "Sonnet 5.5", available: true, source: "claude_native" },
      ],
    };
    let now: Record<string, unknown> = { ...own, pending_model_id: "opus" };
    const c = computer({ sheet: "model" }, { get_thread_runtime: () => ({ runtime: runtime(now) }) });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("text:Opus 5.5 after this turn");
    now = { ...own, failed_model_id: "opus", failed_reason: "Claude's model picker did not show as expected" };
    await app.waitFor("text:Could not switch to Opus 5.5: Claude's model picker did not show as expected", { timeout: 6_000 });
    expect(app.count("id:model-current-sonnet")).toBe(1);
  }, 20_000);

  test("after a refusal the next pick is taken as it is", async () => {
    let refused = true;
    const c = computer({ sheet: "model" }, {
      get_thread_runtime: () => ({ runtime: runtime(claude) }),
      list_providers: () => claudeCatalog,
      set_thread_runtime: (body) => (refused ? { error: "thread/resume failed: no rollout found" } : applied(body, claude)),
    });
    const app = await open(HARNESS, c);
    await app.press("id:harness-open");
    await app.waitFor("id:model-row-sonnet");
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:thread/resume failed: no rollout found");
    refused = false;
    await app.press("id:model-row-sonnet");
    await app.waitFor("text:closes:1");
    expect(c.asked.filter((a) => a.type === "set_thread_runtime").length).toBe(2);
  }, 20_000);
});
