import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { ProviderError, providerMutationRequiresRefresh } from "../providers/errors";
import type { StoredServer } from "../storedServerContract";
import { FakeSocket, installMemoryPlatform } from "../testing/memoryPlatform";
import { Transport, transportTiming } from "../transport";
import { effectRowsForRuntime, threadRuntimeRows } from "./sessionModelHelpers";
import {
  canSwitchModel,
  getThreadRuntime,
  listProviders,
  setThreadRuntime,
  threadRuntimeCatalog,
} from "./sessionRequests";

const server = { id: "s1", name: "Desk", url: "ws://127.0.0.1:9/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const saved = { ...transportTiming };
let sockets: FakeSocket[];
/** Called once the transport opens a socket: the transport signs its connection first, which takes as long as it takes. */
let socketOpened: (() => void)[];

beforeEach(() => {
  sockets = [];
  socketOpened = [];
  installMemoryPlatform({
    openSocket: (url, protocols) => {
      const s = new FakeSocket(url, protocols);
      sockets.push(s);
      for (const opened of socketOpened.splice(0)) opened();
      return s;
    },
  });
});
afterEach(() => Object.assign(transportTiming, saved));

/** The transport's socket number `index`, once the transport has opened it. */
function socketAt(index: number): Promise<FakeSocket> {
  if (sockets.length > index) return Promise.resolve(sockets[index]);
  return new Promise((resolve) => socketOpened.push(() => void socketAt(index).then(resolve)));
}

/** A transport the fake computer has accepted: it waits for the transport's own steps, not for a guessed delay. */
async function connected() {
  const transport = new Transport(server);
  const index = sockets.length;
  const isConnected = new Promise<void>((resolve) => {
    const stop = transport.onState((state) => {
      if (state !== "connected") return;
      stop();
      resolve();
    });
  });
  transport.connect();
  const socket = await socketAt(index);
  socket.open();
  await isConnected;
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

  test("a choice the agent takes after its turn is applied, not an invalid reply", async () => {
    // 11-24: an agent that takes the pick when its turn ends echoes what it runs on now.
    const { transport, answer } = await connected();
    const done = setThreadRuntime(transport, "w1", { connectionId: "c1", modelId: "gpt-6-mini", effect: "low" });
    answer("thread_runtime_set", {
      runtime: runtime({ pending_model_id: "gpt-6-mini", pending_reasoning_effort: "low" }),
      persistence_outcome: "applied", persistence_durable: true,
    });
    const result = await done;
    expect(result.runtime.model_id).toBe("gpt-6");
    expect(result.runtime.pending_model_id).toBe("gpt-6-mini");
    const wrong = setThreadRuntime(transport, "w1", { connectionId: "c1", modelId: "gpt-6-mini", effect: "high" });
    answer("thread_runtime_set", {
      runtime: runtime({ pending_model_id: "gpt-6-mini", pending_reasoning_effort: "low" }),
      persistence_outcome: "applied", persistence_durable: true,
    });
    await expect(wrong).rejects.toMatchObject({ kind: "invalid" });
    transport.close();
  });

  test("a busy Session fails as busy: try again without a refresh", async () => {
    const { transport, answer } = await connected();
    const done = setThreadRuntime(transport, "w1", { connectionId: "c1", modelId: "gpt-6" });
    answer("error", { code: "route_binding_busy", message: "session route busy" });
    const error = await done.catch((e) => e);
    expect(error).toMatchObject({ kind: "busy", message: "Session Model selection busy" });
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
    transport.close();
  });
});
