import { afterAll, beforeEach, describe, expect, mock, spyOn, test } from "bun:test";

// Pins the request/response contract of every correlated RPC on the
// WebSocket client: the frame it sends, the reply that settles it, which
// error channels reject it, its timeout, how disconnect and an offline client
// end it, and which fields an error carries.

mock.module("react-native", () => ({ Platform: { OS: "web" } }));
mock.module("./auth", () => ({
  buildAuthorizationHeader: async () => "test-authorization",
}));
mock.module("./connectionIssue", () => ({
  diagnoseConnectionIssue: async () => null,
}));

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readyState = FakeWebSocket.CONNECTING;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }

  send(value: string) {
    if (this.readyState !== FakeWebSocket.OPEN) {
      throw new Error("socket is not open");
    }
    this.sent.push(value);
  }

  receive(message: object) {
    this.onmessage?.({ data: JSON.stringify(message) });
  }

  close() {
    if (this.readyState === FakeWebSocket.CLOSED) return;
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }
}

const originalWebSocket = globalThis.WebSocket;
Object.assign(globalThis, { WebSocket: FakeWebSocket });

const { WebSocketClient } = await import("./websocket");
type Client = InstanceType<typeof WebSocketClient>;

const server = {
  id: "server-a",
  name: "Server A",
  url: "ws://server-a.test/ws",
  daemonId: "daemon-a",
  daemonPublicKey: "public-key-a",
};

const PROBE_ERROR_CHANNELS = [
  "error",
  "skills_inventory_error",
  "skills_command_error",
  "skills_mutation_error",
  "skills_inspect_error",
  "plugins_inventory_error",
  "plugin_command_error",
  "plugin_mutation_error",
];

const persistence = { persistence_outcome: "applied", persistence_durable: true };
const catalog = {
  revision: 1,
  connections: [
    { id: "conn", name: "Conn", preset_id: "custom", clients: ["codex"], credential_ready: true },
  ],
  defaults: { codex: { connection_id: "conn" } },
  presets: [{ id: "custom", label: "Custom", clients: ["codex"], advanced: true }],
  models: { conn: [{ id: "model", available: true, source: "discovered" }] },
};
const runtime = {
  session_id: "worker",
  client: "codex",
  connection_id: "conn",
  connection_name: "Conn",
  provider_label: "Conn",
  model_id: "model",
  credential_ready: true,
  hot_switchable: true,
};
const skillIdentity = {
  operation: "delete" as const,
  skillId: "a".repeat(24),
  skillName: "useful",
  rootPath: "/home/test/.codex/skills/useful",
  canonicalPath: "/home/test/.codex/skills/useful",
  allowedRoot: "/home/test/.codex/skills",
  cwd: "/workspace",
};
const pluginInput = { operation: "install" as const, pluginId: "plug", host: "codex" as const, scope: "user" as const };
const fileRequest = { workerId: "worker", processId: 7, startedAt: 1759276800000, path: "/w/a.png" };

type Rpc = {
  name: string;
  call(client: Client): Promise<unknown>;
  reply?: Record<string, unknown>;
};

const RPCS: Rpc[] = [
  { name: "createSession", call: (c) => c.createSession(server.id, { cwd: "/w", name: "n", modelId: " m " }), reply: { type: "session_created", worker_id: "w1" } },
  { name: "listProviders", call: (c) => c.listProviders(server.id), reply: { type: "providers", ...catalog } },
  { name: "upsertProviderConnection", call: (c) => c.upsertProviderConnection(server.id, { connection: { id: "conn" } as any, revision: 1, credential: " key " }), reply: { type: "providers", ...catalog, ...persistence } },
  { name: "deleteProviderConnection", call: (c) => c.deleteProviderConnection(server.id, "conn", 1), reply: { type: "providers", ...catalog, ...persistence } },
  { name: "setProviderConnection", call: (c) => c.setProviderConnection(server.id, { client: "codex", connectionId: "conn", revision: 1 } as any), reply: { type: "providers", ...catalog, ...persistence } },
  { name: "switchProvider", call: (c) => c.switchProvider(server.id, { client: "codex", connectionId: "conn", revision: 1 } as any), reply: { type: "providers", ...catalog, ...persistence } },
  { name: "setProviderModels", call: (c) => c.setProviderModels(server.id, { connectionId: "conn", modelIds: ["model"] }), reply: { type: "providers", ...catalog, ...persistence } },
  { name: "discoverProviderModels", call: (c) => c.discoverProviderModels(server.id, "conn"), reply: { type: "provider_models", connection_id: "conn", models: [{ id: "model" }] } },
  { name: "refreshModelsDevMetadata", call: (c) => c.refreshModelsDevMetadata(server.id), reply: { type: "models_dev_metadata_refresh", ok: true } },
  { name: "testProviderConnection", call: (c) => c.testProviderConnection(server.id, { client: "codex", baseUrl: "https://x", apiKey: "k" }), reply: { type: "provider_connection_test", ok: true, client: "codex" } },
  { name: "testSavedProviderConnection", call: (c) => c.testSavedProviderConnection(server.id, "conn"), reply: { type: "provider_connection_test", ok: true, client: "codex" } },
  { name: "getThreadRuntime", call: (c) => c.getThreadRuntime(server.id, "worker"), reply: { type: "thread_runtime", runtime } },
  { name: "setThreadRuntime", call: (c) => c.setThreadRuntime(server.id, { workerId: "worker", runtime: { connectionId: "conn", modelId: "model" } as any }), reply: { type: "thread_runtime_set", runtime, ...persistence } },
  { name: "setProviderCredential", call: (c) => c.setProviderCredential(server.id, "conn", " secret "), reply: { type: "provider_credential", connection_id: "conn", credential_ready: true, ...persistence } },
  { name: "listDir", call: (c) => c.listDir(server.id, "/w"), reply: { type: "dir_list", path: "/w", entries: [{ name: "a", path: "/w/a" }] } },
  { name: "getGitDiffStatus", call: (c) => c.getGitDiffStatus(server.id, { targetId: "t", cwd: "/w" }), reply: { type: "git_diff_status" } },
  { name: "getGitDiffPatch", call: (c) => c.getGitDiffPatch(server.id, { targetId: "t", cwd: "/w", path: "a" }), reply: { type: "git_diff_patch", patch: { path: "a" } } },
  { name: "getGitDiffPage", call: (c) => c.getGitDiffPage(server.id, { targetId: "t", cwd: "/w", path: "a", scope: "all", row: 0 } as any), reply: { type: "git_diff_page", page: { path: "a" } } },
  { name: "getGitDiffFileContent", call: (c) => c.getGitDiffFileContent(server.id, { targetId: "t", cwd: "/w", path: "a" }), reply: { type: "git_diff_file_content", content: { path: "a" } } },
  { name: "getGitRepoEntries", call: (c) => c.getGitRepoEntries(server.id, { targetId: "t", cwd: "/w", path: "a" }), reply: { type: "git_repo_entries", browser: { path: "a" } } },
  { name: "getGitRepoFileContent", call: (c) => c.getGitRepoFileContent(server.id, { targetId: "t", cwd: "/w", path: "a" }), reply: { type: "git_repo_file_content", content: { path: "a" } } },
  { name: "getCodexSlashCommands", call: (c) => c.getCodexSlashCommands(server.id), reply: { type: "codex_slash_commands", version: "1", commands: [{ value: "/x", name: "x" }, { value: "y", name: "y" }] } },
  { name: "getCodexSkills", call: (c) => c.getCodexSkills(server.id, { cwd: "/w" }), reply: { type: "codex_skills", skills: [{ name: "s", path: "/p" }, { name: "" }] } },
  { name: "getSkillsInventory", call: (c) => c.getSkillsInventory(server.id, { cwd: "/w", generation: 3 }), reply: { type: "skills_inventory", generation: 2, inventory: {} } },
  { name: "buildSkillsCommand", call: (c) => c.buildSkillsCommand(server.id, skillIdentity), reply: { type: "skills_command", command: {} } },
  { name: "executeSkillsMutation", call: (c) => c.executeSkillsMutation(server.id, skillIdentity), reply: { type: "skills_mutation_result" } },
  { name: "getSkillsInspect", call: (c) => c.getSkillsInspect(server.id, { skillName: "useful", generation: 1 }), reply: { type: "skills_inspect_result", generation: 0 } },
  { name: "getPluginsInventory", call: (c) => c.getPluginsInventory(server.id, { generation: 1 }), reply: { type: "plugins_inventory", generation: 0 } },
  { name: "buildPluginCommand", call: (c) => c.buildPluginCommand(server.id, pluginInput as any), reply: { type: "plugin_command", command: {} } },
  { name: "executePluginMutation", call: (c) => c.executePluginMutation(server.id, pluginInput as any), reply: { type: "plugin_mutation_result" } },
  { name: "getCodexTerminalSnapshot", call: (c) => c.getCodexTerminalSnapshot(server.id, "t"), reply: { type: "codex_terminal_snapshot", text: "out" } },
  { name: "getCodexAsset", call: (c) => c.getCodexAsset(server.id, { path: "/a.png", cwd: "/w" }), reply: { type: "codex_asset", data_url: "data:image/png;base64,AA" } },
  { name: "dshInteraction", call: (c) => c.dshInteraction(server.id, fileRequest), reply: { type: "dsh_interaction", result: { epoch: "e", connected: true, items: [] } } },
  { name: "dshInteraction answer", call: (c) => c.dshInteraction(server.id, fileRequest, { id: "q" } as any), reply: { type: "dsh_interaction", result: { accepted: false } } },
  { name: "getSessionImage", call: (c) => c.getSessionImage(server.id, fileRequest), reply: { type: "session_image", data_url: "data:image/png;base64,AA" } },
  { name: "getSessionFileMetadata", call: (c) => c.getSessionFileMetadata(server.id, fileRequest), reply: { type: "session_file_metadata", metadata: {} } },
  { name: "getSessionFileText", call: (c) => c.getSessionFileText(server.id, { ...fileRequest, generation: "g" }), reply: { type: "session_file_text", text: {} } },
  { name: "getTerminalSnapshot", call: (c) => c.getTerminalSnapshot(server.id, "t"), reply: { type: "terminal_snapshot", text: "x", target_id: "t" } },
  { name: "getTerminalHistory", call: (c) => c.getTerminalHistory(server.id, "s"), reply: { type: "terminal_history", session_id: "s", history: { lines: [] } } },
  { name: "answerChoice", call: (c) => c.answerChoice(server.id, "worker", { call_id: "c", answers: [{ selected: [0] }] }), reply: { type: "choice_answered", answers: ["A"] } },
  { name: "sendKey", call: (c) => c.sendKey(server.id, "worker", "Enter"), reply: { type: "key_sent" } },
  { name: "getStats", call: (c) => c.getStats(server.id), reply: { type: "stats_data", total: 1 } },
  { name: "getResourceTelemetry", call: (c) => c.getResourceTelemetry(server.id), reply: { type: "resource_telemetry" } },
  { name: "actOnBrainWork", call: (c) => c.actOnBrainWork(server.id, "work", "reply" as any, { text: "hi" }), reply: { type: "brain_work_action", status: "ok", admission: "" } },
  { name: "getBrainContext", call: (c) => c.getBrainContext(server.id), reply: { type: "brain_context" } },
  { name: "runBrainGC", call: (c) => c.runBrainGC(server.id), reply: { type: "brain_gc", housekeeping: { removed: 1 } } },
  { name: "startNewBrainChat", call: (c) => c.startNewBrainChat(server.id), reply: { type: "brain_snapshot" } },
  { name: "requestConnections", call: (c) => c.requestConnections(server.id, { action: "list" } as any), reply: { type: "connections_result", connections: { accounts: [] } } },
  { name: "getTelegramConnectionStatus", call: (c) => c.getTelegramConnectionStatus(server.id), reply: { type: "telegram_connection_status", connection: { state: "ready", enabled: true } } },
  { name: "configureTelegramConnection", call: (c) => c.configureTelegramConnection(server.id, "token"), reply: { type: "telegram_connection_status", connection: { state: "ready" } } },
  { name: "removeTelegramConnection", call: (c) => c.removeTelegramConnection(server.id), reply: { type: "telegram_connection_status", connection: { state: "ready", enabled: false } } },
  { name: "beginTelegramBinding", call: (c) => c.beginTelegramBinding(server.id), reply: { type: "telegram_binding_challenge", challenge: { url: "https://t.me/x", expires_at: "z" } } },
  { name: "setBrainExecutor", call: (c) => c.setBrainExecutor(server.id, "claude"), reply: { type: "brain_snapshot", brain: { host_executor: { id: "claude" } } } },
  { name: "getBrainWorkspaceTree", call: (c) => c.getBrainWorkspaceTree(server.id, "notes"), reply: { type: "brain_workspace_tree", workspace_tree: { entries: [{ name: "a" }, {}] } } },
  { name: "getBrainWorkspaceFile", call: (c) => c.getBrainWorkspaceFile(server.id, "notes/a.md"), reply: { type: "brain_workspace_file", file: { name: "a.md" } } },
  { name: "serviceTunnel", call: (c) => c.serviceTunnel(server.id, "svc", "gen", "start"), reply: { type: "service_tunnel", service_id: "svc", tunnel: { generation: "gen", state: "up" } } },
  { name: "listSessionServices", call: (c) => c.listSessionServices(server.id), reply: { type: "session_service_list", generated_at: "t", services: [{ id: "svc", port: 80 }] } },
  { name: "getCalendarItem", call: (c) => c.getCalendarItem(server.id, "cal"), reply: { type: "calendar_item", calendar_item: { id: "cal" } } },
  { name: "createCalendarItem", call: (c) => c.createCalendarItem(server.id, { title: "t" } as any), reply: { type: "calendar_item_created", calendar_item: { id: "cal" } } },
  { name: "updateCalendarItem", call: (c) => c.updateCalendarItem(server.id, { id: "cal", revision: 2 } as any), reply: { type: "calendar_item_updated", calendar_item: { id: "cal" } } },
  { name: "cancelCalendarItem", call: (c) => c.cancelCalendarItem(server.id, "cal", 2), reply: { type: "calendar_item_cancelled", calendar_item: { id: "cal" } } },
  { name: "runCalendarItem", call: (c) => c.runCalendarItem(server.id, "cal"), reply: { type: "calendar_item_running", calendar_item: { id: "cal" } } },
  { name: "writeWorkItem", call: (c) => c.writeWorkItem(server.id, { project: "p", body: "b" }), reply: { type: "work_item_written", work_item: { id: "w" } } },
  { name: "deleteWorkItem", call: (c) => c.deleteWorkItem(server.id, "w"), reply: { type: "work_item_deleted_ack" } },
];

type Timer = { fn: () => void; ms: number; cleared: boolean };
let timers: Timer[] = [];

beforeEach(() => {
  FakeWebSocket.instances = [];
  timers = [];
});

afterAll(() => {
  Object.assign(globalThis, { WebSocket: originalWebSocket });
});

async function flush() {
  for (let i = 0; i < 5; i += 1) await Promise.resolve();
}

async function connected(): Promise<{ client: Client; socket: FakeWebSocket }> {
  const client = new WebSocketClient();
  const index = FakeWebSocket.instances.length;
  client.connect(server);
  await flush();
  const socket = FakeWebSocket.instances[index];
  if (!socket) throw new Error("expected a socket");
  socket.open();
  return { client, socket };
}

function handlerCount(client: Client) {
  const handlers = (client as unknown as { handlers: Map<string, unknown[]> }).handlers;
  return [...handlers.values()].reduce((total, list) => total + list.length, 0);
}

function describeError(error: unknown) {
  const value = error as Error & { code?: unknown; current?: unknown; kind?: unknown; retryable?: unknown };
  return {
    name: value?.name,
    message: value?.message,
    ...(value?.code !== undefined ? { code: value.code } : {}),
    ...(value?.current !== undefined ? { current: value.current } : {}),
  };
}

function track(promise: Promise<unknown>) {
  const state: { outcome: unknown } = { outcome: "pending" };
  promise.then(
    (value) => {
      const resolved =
        value && typeof value === "object" && "request_id" in value
          ? { ...value, request_id: "#" }
          : value;
      state.outcome = { resolved: resolved === undefined ? "undefined" : resolved };
    },
    (error) => { state.outcome = { rejected: describeError(error) }; },
  );
  return state;
}

/** Captures timers the request arms so a timeout can be fired on demand. */
function withTimers<T>(run: () => T): T {
  const set = spyOn(globalThis, "setTimeout").mockImplementation(((fn: () => void, ms: number) => {
    timers.push({ fn, ms, cleared: false });
    return timers.length as unknown as ReturnType<typeof setTimeout>;
  }) as typeof setTimeout);
  try {
    return run();
  } finally {
    set.mockRestore();
  }
}

async function settleByTimeout(state: { outcome: unknown }) {
  if (state.outcome === "pending") {
    timers.at(-1)?.fn();
    await flush();
  }
}

async function probe(rpc: Rpc) {
  const result: Record<string, unknown> = {};

  // Success, with a stale reply first.
  {
    const { client, socket } = await connected();
    const state = track(withTimers(() => rpc.call(client)));
    const frame = JSON.parse(socket.sent.at(-1) ?? "{}");
    const requestId = String(frame.request_id ?? "");
    result.request = {
      ...frame,
      request_id: requestId.replace(/[0-9a-z]+_[0-9a-z]+$/, "#_#"),
    };
    result.timeoutMs = timers.at(-1)?.ms;
    if (rpc.reply) {
      socket.receive({ ...rpc.reply, request_id: "stale" });
      await flush();
      result.staleReply = state.outcome;
      socket.receive({ ...rpc.reply, request_id: requestId });
      await flush();
    }
    result.reply = state.outcome;
    await settleByTimeout(state);
    result.handlersAfterReply = handlerCount(client);
    client.disconnect();
  }

  // Each error channel, with message/code/current and without a message.
  {
    const rejecting: Record<string, unknown> = {};
    const ignored: string[] = [];
    let fallback: unknown;
    for (const channel of PROBE_ERROR_CHANNELS) {
      const { client, socket } = await connected();
      const state = track(withTimers(() => rpc.call(client)));
      const requestId = JSON.parse(socket.sent.at(-1) ?? "{}").request_id;
      socket.receive({ type: channel, request_id: requestId, message: "probe failure", code: "probe_code", current: { id: "now" } });
      await flush();
      if (state.outcome === "pending") {
        ignored.push(channel);
        await settleByTimeout(state);
      } else {
        rejecting[channel] = state.outcome;
        if (fallback === undefined) {
          const second = track(withTimers(() => rpc.call(client)));
          const secondId = JSON.parse(socket.sent.at(-1) ?? "{}").request_id;
          socket.receive({ type: channel, request_id: secondId });
          await flush();
          fallback = second.outcome;
          await settleByTimeout(second);
        }
      }
      expect(handlerCount(client)).toBe(0);
      client.disconnect();
    }
    result.errorChannels = rejecting;
    result.ignoredErrorChannels = ignored;
    result.errorWithoutMessage = fallback;
  }

  // Timeout.
  {
    const { client } = await connected();
    const state = track(withTimers(() => rpc.call(client)));
    timers.at(-1)?.fn();
    await flush();
    result.timeout = state.outcome;
    result.handlersAfterTimeout = handlerCount(client);
    client.disconnect();
  }

  // Intentional disconnect while pending.
  {
    const { client } = await connected();
    const state = track(withTimers(() => rpc.call(client)));
    client.disconnect();
    await flush();
    result.disconnect = state.outcome;
    await settleByTimeout(state);
    result.handlersAfterDisconnect = handlerCount(client);
  }

  // Never connected.
  {
    const client = new WebSocketClient();
    let state: { outcome: unknown };
    try {
      state = track(withTimers(() => rpc.call(client)));
    } catch (error) {
      state = { outcome: { threw: describeError(error) } };
    }
    await flush();
    result.offline = state.outcome;
    await settleByTimeout(state);
    result.handlersAfterOffline = handlerCount(client);
  }

  return result;
}

describe("correlated WebSocket requests", () => {
  for (const rpc of RPCS) {
    test(rpc.name, async () => {
      expect(await probe(rpc)).toMatchSnapshot();
    });
  }
});
