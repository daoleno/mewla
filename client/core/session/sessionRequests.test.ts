import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { activationBusyAllowsRetryWithoutRefresh, ProviderError, providerMutationRequiresRefresh } from "../providers/errors";
import type { StoredServer } from "../storedServerContract";
import { FakeSocket, installMemoryPlatform } from "../testing/memoryPlatform";
import { Transport, transportTiming } from "../transport";
import { effectRowsForRuntime, threadRuntimeRows } from "./sessionModelHelpers";
import {
  canSwitchModel,
  chatSlashCommands,
  codexSkills,
  codexSlashCommands,
  codexStatus,
  filterCodexSkills,
  getThreadRuntime,
  listProviders,
  setThreadRuntime,
  threadRuntimeCatalog,
} from "./sessionRequests";

const server = { id: "s1", name: "Desk", url: "ws://127.0.0.1:9/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const wait = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));
const saved = { ...transportTiming };
let sockets: FakeSocket[];

beforeEach(() => {
  sockets = [];
  installMemoryPlatform({ openSocket: (url, protocols) => { const s = new FakeSocket(url, protocols); sockets.push(s); return s; } });
});
afterEach(() => Object.assign(transportTiming, saved));

async function connected() {
  const transport = new Transport(server);
  transport.connect();
  await wait();
  const socket = sockets.at(-1)!;
  socket.open();
  const last = () => JSON.parse(socket.sent.at(-1)!);
  const answer = (type: string, body: Record<string, unknown>) => socket.receive({ type, request_id: last().request_id, ...body });
  return { transport, socket, last, answer };
}

const runtime = (patch: Record<string, unknown> = {}) => ({
  session_id: "w1", client: "codex", connection_id: "c1", connection_name: "OpenAI", model_id: "gpt-6",
  reasoning_effort: "", reasoning_effort_default: "medium", reasoning_efforts: ["low", "medium", "high"],
  credential_ready: true, hot_switchable: true, ...patch,
});
const catalog = {
  revision: 3,
  connections: [{ id: "c1", name: "OpenAI", clients: ["codex"], credential_ready: true, advanced: false }],
  defaults: { codex: { connection_id: "c1" } },
  presets: [],
  models: { c1: [{ id: "gpt-6", available: true, source: "catalog", reasoning_efforts: ["low", "medium", "high"], reasoning_effort_default: "medium" }, { id: "gpt-6-mini", available: true, source: "catalog" }] },
};

describe("the Model sheet's requests", () => {
  test("canSwitchModel needs a managed Model that switches live", () => {
    expect(canSwitchModel({ structured_events: true, model_profile_managed: true, model_profile_active_switch: true })).toBe(true);
    expect(canSwitchModel({ structured_events: true, model_profile_managed: true, model_profile_active_switch: false })).toBe(false);
    expect(canSwitchModel(null)).toBe(false);
  });

  test("reads the Session's runtime and the Providers it lists rows from", async () => {
    const { transport, last, answer } = await connected();
    const selection = getThreadRuntime(transport, "w1");
    expect(last()).toMatchObject({ type: "get_thread_runtime", worker_id: "w1" });
    answer("thread_runtime", { runtime: runtime() });
    const read = await selection;
    expect(read.model_id).toBe("gpt-6");
    const snapshot = threadRuntimeCatalog(transport, read);
    expect(last().type).toBe("list_providers");
    answer("providers", catalog);
    const rows = threadRuntimeRows({ snapshot: await snapshot, selection: read });
    expect(rows.map((r) => [r.modelId, r.current, r.effects])).toEqual([
      ["gpt-6", true, ["", "low", "medium", "high"]],
      ["gpt-6-mini", false, [""]],
    ]);
    expect(effectRowsForRuntime(rows[0]).map((r) => [r.key, r.selected])).toEqual([["default", true], ["low", false], ["medium", false], ["high", false]]);
    transport.close();
  });

  test("a runtime for another Session is an invalid reply", async () => {
    const { transport, answer } = await connected();
    const selection = getThreadRuntime(transport, "w1");
    answer("thread_runtime", { runtime: runtime({ session_id: "w2" }) });
    await expect(selection).rejects.toMatchObject({ kind: "invalid" });
    transport.close();
  });

  test("a dsh Session's models come with its runtime, without asking for Providers", async () => {
    const { transport, socket } = await connected();
    const before = socket.sent.length;
    const snapshot = await threadRuntimeCatalog(transport, {
      session_id: "w1", client: "dsh", connection_id: "dsh", connection_name: "dsh", model_id: "m1",
      credential_ready: true, hot_switchable: true, native_models: [{ id: "m1", available: true, source: "native" }],
    });
    expect(socket.sent.length).toBe(before);
    expect(snapshot.models.dsh.map((m) => m.id)).toEqual(["m1"]);
    expect(snapshot.connections[0].clients).toEqual(["dsh"]);
    transport.close();
  });

  test("switching sends the model and effect, and settles on the applied runtime", async () => {
    const { transport, last, answer } = await connected();
    const done = setThreadRuntime(transport, "w1", { connectionId: "c1", modelId: "gpt-6", effect: "high" });
    expect(last()).toMatchObject({ type: "set_thread_runtime", worker_id: "w1", runtime: { connection_id: "c1", model_id: "gpt-6", effect: "high" } });
    answer("thread_runtime_set", { runtime: runtime({ reasoning_effort: "high" }), persistence_outcome: "applied", persistence_durable: true });
    const result = await done;
    expect(result.runtime.reasoning_effort).toBe("high");
    expect(result.persistence).toMatchObject({ applied: true, durable: true });

    const byDefault = setThreadRuntime(transport, "w1", { connectionId: "c1", modelId: "gpt-6", useDefaultEffect: true });
    expect(last().runtime).toEqual({ connection_id: "c1", model_id: "gpt-6", use_default_effect: true });
    answer("thread_runtime_set", { runtime: runtime(), persistence_outcome: "applied", persistence_durable: false });
    expect((await byDefault).persistence.durable).toBe(false);
    transport.close();
  });

  test("a switch reply that echoes another model, or no persistence, is not taken as applied", async () => {
    const { transport, answer } = await connected();
    const other = setThreadRuntime(transport, "w1", { connectionId: "c1", modelId: "gpt-6-mini" });
    answer("thread_runtime_set", { runtime: runtime(), persistence_outcome: "applied", persistence_durable: true });
    await expect(other).rejects.toMatchObject({ kind: "invalid" });
    const silent = setThreadRuntime(transport, "w1", { connectionId: "c1", modelId: "gpt-6" });
    answer("thread_runtime_set", { runtime: runtime() });
    const error = await silent.catch((e) => e);
    expect(error).toBeInstanceOf(ProviderError);
    expect(providerMutationRequiresRefresh(error)).toBe(true);
    transport.close();
  });

  test("a busy Session fails as busy: try again without a refresh", async () => {
    const { transport, answer } = await connected();
    const done = setThreadRuntime(transport, "w1", { connectionId: "c1", modelId: "gpt-6" });
    answer("error", { code: "route_binding_busy", message: "session route busy" });
    const error = await done.catch((e) => e);
    expect(error).toMatchObject({ kind: "busy", message: "Session Model selection busy" });
    expect(activationBusyAllowsRetryWithoutRefresh(error)).toBe(true);
    transport.close();
  });

  test("offline is a ProviderError of its own, before anything is sent", async () => {
    const transport = new Transport(server);
    await expect(getThreadRuntime(transport, "w1")).rejects.toMatchObject({ kind: "offline" });
    await expect(setThreadRuntime(transport, "w1", { connectionId: "c1", modelId: "m" })).rejects.toMatchObject({ kind: "offline" });
    await expect(listProviders(transport)).rejects.toMatchObject({ kind: "offline" });
  });

  test("a timeout says what timed out", async () => {
    const { transport } = await connected();
    // The transport's own timer: shorten it by asking through a timed-out request.
    const original = transport.request.bind(transport);
    transport.request = ((type: string, body: Record<string, unknown>, options: { reply: string }) => original(type, body, { ...options, timeoutMs: 5 })) as typeof transport.request;
    await expect(setThreadRuntime(transport, "w1", { connectionId: "c1", modelId: "m" })).rejects.toMatchObject({ kind: "timeout", message: "Timed out while switching model." });
    await expect(codexStatus(transport, "w1")).rejects.toMatchObject({ code: "timeout", message: "Timed out while loading Codex status." });
    transport.close();
  });
});

describe("Codex requests", () => {
  test("status asks for the chat by target id and keeps the daemon's code on failure", async () => {
    const { transport, last, answer } = await connected();
    const done = codexStatus(transport, "w1");
    expect(last()).toMatchObject({ type: "codex_status", target_id: "w1" });
    answer("codex_status", { status: { thread: { id: "th", model: "gpt-6" }, limits: [] } });
    expect((await done).thread?.model).toBe("gpt-6");
    const failed = codexStatus(transport, "w2");
    answer("error", { code: "codex_status_unavailable", message: "not started by mewla" });
    await expect(failed).rejects.toMatchObject({ code: "codex_status_unavailable", message: "not started by mewla" });
    transport.close();
  });

  test("skills for a folder drop the nameless and default scope and enabled", async () => {
    const { transport, last, answer } = await connected();
    const done = codexSkills(transport, "/repo");
    expect(last()).toMatchObject({ type: "codex_skills", cwd: "/repo" });
    answer("codex_skills", { skills: [{ name: "deploy", path: "/s/deploy", description: "Ship it" }, { name: "", path: "/x" }, { name: "lint", path: "/s/lint", scope: "repo", enabled: false }] });
    const snapshot = await done;
    expect(snapshot.cwd).toBe("/repo");
    expect(snapshot.skills).toEqual([
      { name: "deploy", path: "/s/deploy", description: "Ship it", scope: "user", enabled: true },
      { name: "lint", path: "/s/lint", description: undefined, scope: "repo", enabled: false },
    ]);
    expect(filterCodexSkills(snapshot.skills, "SHIP").map((s) => s.name)).toEqual(["deploy"]);
    expect(filterCodexSkills(snapshot.skills, "repo").map((s) => s.name)).toEqual(["lint"]);
    expect(filterCodexSkills(snapshot.skills, "  ").length).toBe(2);
    transport.close();
  });

  test("slash commands drop malformed ones; the chat list applies ChatUI's overrides", async () => {
    const { transport, answer } = await connected();
    const raw = codexSlashCommands(transport);
    const commands = [
      { value: "/status", name: "status", title: "Status", description: "Show status", category: "session", execution: "terminal-required", chat_supported: false },
      { value: "/compact", name: "compact", category: "session", execution: "native", chat_supported: true },
      { value: "review", name: "review" },
      { value: "/model", name: "model", category: "settings", execution: "unsupported", input: { kind: "picker", required: true } },
    ];
    answer("codex_slash_commands", { source: "codex", commands });
    const snapshot = await raw;
    expect(snapshot.commands.map((c) => c.name)).toEqual(["status", "compact", "model"]);
    expect(snapshot.commands[2]).toMatchObject({ terminal_supported: false, input: { kind: "picker", required: true } });
    const built = chatSlashCommands(transport);
    answer("codex_slash_commands", { commands });
    const list = await built;
    expect(list.map((c) => c.name)).toEqual(["status", "model"]);
    expect(list[0]).toMatchObject({ execution: "native", chat_supported: true });
    transport.close();
  });
});
