import { describe, expect, open, test } from "nsh/test";
import { answering, pressUntil, pressWhenEnabled, typeUntil } from "../testing/fixtures";

// Brain's own things as components (soft-final/SPEC.md §1.3, §1.4, §1.6), driven on their own: the
// ⋯ menu and its pages, the can't-start notice, the thread states, and what the composer hosts.
const HARNESS = "./testing/Harness.tsx";

const claude = { id: "claude", name: "claude", provider: "claude", runtime: "tmux", capabilities: { interactive_tty: true, structured_events: true } };
const codex = { id: "codex", name: "codex", provider: "codex", runtime: "tmux", capabilities: { interactive_tty: true, structured_events: true } };
const agent = { id: "agent", name: "agent", provider: "cursor", runtime: "tmux", capabilities: { interactive_tty: true, structured_events: true } };
const grok = { id: "grok", name: "grok", provider: "grok", runtime: "tmux", capabilities: { interactive_tty: true, structured_events: true } };
const switchable = { structured_events: true, model_profile_managed: true, model_profile_active_switch: true };
const hostWorker = { id: "brain-host", name: "Brain", status: "running", provider: "claude", cwd: "/home/me/brain", command: "claude", process_id: 42, started_at: 1_700_000_000_000, capabilities: switchable };
const brain = {
  chat_thread_id: "t-now",
  host_worker: hostWorker,
  host_executor: claude,
  executors: [claude, codex, agent, grok],
  executor_states: {
    claude: { installed: true, signed_in: true, account: "me@x" },
    codex: { installed: true, signed_in: true },
    agent: { installed: true, signed_in: false },
    grok: { installed: false, signed_in: false },
  },
};
const runtime = { session_id: "brain-host", client: "claude", connection_id: "c1", connection_name: "Anthropic", model_id: "claude-sonnet-5-5", reasoning_effort: "high", reasoning_efforts: ["low", "high"], credential_ready: true, hot_switchable: true };
const catalog = { revision: 1, connections: [{ id: "c1", name: "Anthropic", clients: ["claude"], credential_ready: true, advanced: false }], defaults: { claude: { connection_id: "c1" } }, presets: [], models: { c1: [{ id: "claude-sonnet-5-5", available: true, source: "catalog" }] } };

type Answer = (body: Record<string, unknown>) => unknown;

function computer(params: Record<string, unknown>, answers: Record<string, Answer> = {}, handlers: Record<string, (data: any) => unknown> = {}) {
  const asked: { type: string; body: Record<string, unknown> }[] = [];
  const all: Record<string, Answer> = { get_thread_runtime: () => ({ runtime }), list_providers: () => catalog, ...answers };
  return {
    asked,
    of: (type: string) => asked.filter((entry) => entry.type === type).map((entry) => entry.body),
    host: {
      params: { brain, ...params },
      handlers: {
        "app.daemon": ({ type, body }: { type: string; body: Record<string, unknown> }) => {
          asked.push({ type, body });
          const answer = all[type];
          return answer ? answer(body) : { error: `unexpected ${type}` };
        },
        ...handlers,
      },
    },
  };
}

/** What the callbacks heard, in order (`harness-out`). */
const said = (app: { texts(): string[] }) => (app.texts().find((text) => text.startsWith("out:")) ?? "out:").slice(4);

async function menu(c: ReturnType<typeof computer>) {
  const app = await open(HARNESS, { platform: "android", host: c.host });
  await app.press("id:harness-open");
  await app.waitFor("id:brain-menu-sheet");
  return app;
}

describe("Brain's menu (§1.6)", () => {
  test("its rows in order, Model showing the model and effort; a row it cannot do is not drawn", async () => {
    const c = computer({ threadLoaded: false });
    const app = await menu(c);
    // Brain's thread has not loaded: no New chat row yet.
    expect(app.texts().filter((text) => ["Open terminal", "Browse workspace", "Switch executor", "Model", "New chat"].includes(text)))
      .toEqual(["Open terminal", "Browse workspace", "Switch executor", "Model"]);
    await app.waitFor("text:claude-sonnet-5-5 · High");
    expect(app.shows("id:brain-menu-new-chat")).toBe(false);
    expect(app.enabled("id:brain-menu-model")).toBe(true);
  }, 20_000);

  test("Open terminal and Browse workspace close the menu and go where the host says", async () => {
    const app = await menu(computer({}));
    await app.press("id:brain-menu-terminal");
    await app.waitFor("text:closed");
    expect(said(app)).toBe("terminal:brain-host");
    await app.press("id:harness-open");
    await app.press("id:brain-menu-workspace");
    await app.waitUntil(() => said(app) === "terminal:brain-host | workspace");
  }, 20_000);

  test("Switch executor: the installed agents with the install line; a refusal says the computer's words", async () => {
    const c = computer({}, { brain_set_executor: () => ({ error: "brain host executor is locked by environment override" }) });
    const app = await menu(c);
    await app.press("id:brain-menu-executor");
    await app.waitFor("id:brain-host-panel");
    expect(app.texts()).toContain("Brain host");
    expect(app.shows("id:brain-executor-claude")).toBe(true);
    expect(app.shows("id:brain-executor-codex")).toBe(true);
    expect(app.shows("id:brain-executor-agent")).toBe(true);
    expect(app.shows("id:brain-executor-grok")).toBe(false);
    expect(app.shows("id:brain-executor-install")).toBe(true);
    // One line per agent, by product name; no runtime, no lone account; a second line only to ask for a sign-in.
    for (const text of ["Claude Code", "Codex", "Cursor Agent", "Not signed in"]) expect(app.texts()).toContain(text);
    expect(app.texts().some((t) => t.includes("tmux") || t.includes("me@x"))).toBe(false);
    await app.press("id:brain-executor-codex");
    await app.waitFor("id:brain-executor-error");
    expect(app.texts()).toContain("brain host executor is locked by environment override");
    expect(c.of("brain_set_executor").at(-1)).toMatchObject({ executor_id: "codex" });
    // Back returns to the menu.
    await app.press("id:brain-menu-back");
    await app.waitFor("id:brain-menu-terminal");
  }, 20_000);

  test("a switch the computer keeps is done when the new host shows up: the menu closes", async () => {
    const c = computer({}, { brain_set_executor: () => ({ brain }) });
    const app = await menu(c);
    await app.press("id:brain-menu-executor");
    await app.press("id:brain-executor-codex");
    await app.waitFor("text:Switching…");
    await app.host.send("app.snapshot", { ...brain, host_executor: codex, host_worker: { ...hostWorker, provider: "codex" } });
    await app.waitFor("text:closed");
    await app.waitUntil(() => said(app) === "switched");
  }, 20_000);

  test("Model opens the Model & Effort panel for Brain's own Session, in the same sheet", async () => {
    const c = computer({});
    const app = await menu(c);
    await app.press("id:brain-menu-model");
    await app.waitFor("id:model-row-claude-sonnet-5-5");
    expect(c.of("get_thread_runtime").at(-1)).toMatchObject({ worker_id: "brain-host" });
  }, 20_000);

  test("New chat asks first; a refused one says it could not start one", async () => {
    const c = computer({}, { brain_chat_new: () => ({ error: "Brain is busy" }) });
    const app = await menu(c);
    app.onDialog(() => "Cancel");
    await answering(app, () => app.press("id:brain-menu-new-chat"));
    expect(app.dialogs().at(-1)?.title).toBe("Start a new chat?");
    expect(c.of("brain_chat_new")).toHaveLength(0);
    await app.press("id:harness-open");
    app.onDialog(() => "New chat");
    await answering(app, () => app.press("id:brain-menu-new-chat"));
    await app.waitUntil(() => said(app) === "notice:Couldn't start a new chat");
    expect(c.of("brain_chat_new")).toHaveLength(1);
  }, 20_000);
});

describe("Brain's states (§1.3)", () => {
  const problem = { code: "not_signed_in", executor: "agent", detail: "Brain is set to run on agent, but agent is not signed in on this computer.", installed_executors: ["claude", "codex"] };
  const cantStart = { ...brain, host_worker: null, host_executor: agent, host_problem: { ...problem, suggest_executor: "claude" } };

  test("Brain can't start: the computer's reason, Use {suggested} switches to it, and Switch executor is there too", async () => {
    const c = computer({ view: "cant-start", brain: cantStart }, { brain_set_executor: () => ({ brain: cantStart }) });
    const app = await open(HARNESS, { platform: "android", host: c.host });
    await app.waitFor("text:Brain can't start");
    expect(app.texts()).toContain(problem.detail);
    expect(app.texts()).toContain("Use Claude Code");
    await app.press("id:brain-switch-executor");
    await app.waitUntil(() => said(app) === "switch-executor");
    await app.press("id:brain-use-suggested");
    await app.waitFor("text:Switching…");
    expect(c.of("brain_set_executor").at(-1)).toMatchObject({ executor_id: "claude" });
    await app.host.send("app.snapshot", brain);
    await app.waitFor("id:brain-cant-start", { state: "gone" });
    await app.waitUntil(() => said(app) === "switch-executor | switched");
  }, 20_000);

  test("Brain can't start, a refused switch: the computer's words under the buttons", async () => {
    const c = computer({ view: "cant-start", brain: cantStart }, { brain_set_executor: () => ({ error: "claude is not installed" }) });
    const app = await open(HARNESS, { platform: "android", host: c.host });
    await app.press("id:brain-use-suggested");
    await app.waitFor("id:brain-cant-start-error");
    expect(app.texts()).toContain("claude is not installed");
  }, 20_000);

  test("chat isn't available for an agent without chat events: Open terminal, Switch executor", async () => {
    const plain = { id: "plain", name: "plain", provider: "custom", capabilities: { interactive_tty: true, structured_events: false } };
    const c = computer({ view: "unavailable", brain: { ...brain, host_executor: plain } });
    const app = await open(HARNESS, { platform: "android", host: c.host });
    await app.waitFor("text:Chat isn't available for plain");
    await app.press("id:brain-open-terminal");
    await app.press("id:brain-switch-executor");
    await app.waitUntil(() => said(app) === "terminal:brain-host | switch-executor");
  }, 20_000);

  test("a chat that did not load: Try again, Open terminal", async () => {
    const app = await open(HARNESS, { platform: "android", host: computer({ view: "failed" }).host });
    await app.waitFor("text:Couldn't load the chat");
    await app.press("id:brain-chat-try-again");
    await app.press("id:brain-chat-open-terminal");
    await app.waitUntil(() => said(app) === "reload | terminal:brain-host");
  }, 20_000);
});

describe("Brain's composer (§1.4)", () => {
  test("+ › Attach File: the picked file goes with the message", async () => {
    const c = computer({ view: "composer" }, {}, {
      "secure.get": () => null,
      "secure.set": () => undefined,
      "secure.remove": () => undefined,
      "files.pick": () => [{ uri: "content://picker/shot.png", name: "shot.png", mimeType: "image/png", size: 2048 }],
      "files.upload": () => Promise.resolve().then(() => ({ status: 200, body: JSON.stringify({ path: "/state/uploads/shot.png", name: "shot.png" }) })),
      "files.cancel": () => null,
    });
    const app = await open(HARNESS, { platform: "android", host: c.host });
    await pressUntil(app, "id:composer-plus", "id:attach-file");
    // Under a full suite's load a press or the first keys after a sheet closes can be lost: each step waits for what it does.
    await pressUntil(app, "id:attach-file", "text:Ready");
    await typeUntil(app, "id:composer", "What is on this screen?", "id:send");
    await pressWhenEnabled(app, "id:send");
    await app.waitUntil(() => said(app) === "send:What is on this screen? +/state/uploads/shot.png");
  }, 20_000);

  test("typing @ lists the installed agents; a pick puts its handle in the message and the next key goes after it", async () => {
    const app = await open(HARNESS, { platform: "android", host: computer({ view: "composer" }).host });
    await app.type("id:composer", "Ask @");
    await app.waitFor("id:brain-mention-panel");
    expect(app.shows("id:brain-mention-claude")).toBe(true);
    expect(app.shows("id:brain-mention-codex")).toBe(true);
    expect(app.shows("id:brain-mention-agent")).toBe(true);
    expect(app.shows("id:brain-mention-grok")).toBe(false);
    expect(app.texts()).toContain("@codex");
    await app.type("id:composer", "co");
    await app.waitUntil(() => !app.shows("id:brain-mention-claude"));
    await app.press("id:brain-mention-codex");
    await app.waitUntil(() => app.value("id:composer") === "Ask @codex ");
    expect(app.shows("id:brain-mention-panel")).toBe(false);
    await app.type("id:composer", "now");
    await app.waitUntil(() => app.value("id:composer") === "Ask @codex now");
  }, 20_000);

  test("on Codex, / lists its slash commands and /status opens Codex's status instead of sending", async () => {
    const c = computer({ view: "composer", brain: { ...brain, host_worker: { ...hostWorker, provider: "codex" }, host_executor: codex } }, {
      codex_slash_commands: () => ({ commands: [{ value: "/status", name: "status", description: "Show status", category: "session", chat_supported: true }] }),
      codex_status: () => ({ status: { thread: { id: "th-1", model: "gpt-6", effort: "high", cwd: "/home/me/brain" }, limits: [{ id: "codex", windows: [{ used_percent: 25, window_minutes: 300 }] }] } }),
    });
    const app = await open(HARNESS, { platform: "android", host: c.host });
    await app.type("id:composer", "/sta");
    await app.waitFor("id:slash-status");
    await app.type("id:composer", "tus");
    await app.press("id:send");
    await app.waitFor("id:status-meter");
    expect(said(app)).toBe("");
    expect(c.of("codex_status").at(-1)).toMatchObject({ target_id: "brain-host" });
  }, 20_000);

  test("a composer that cannot send says why and sends nothing", async () => {
    const app = await open(HARNESS, { platform: "android", host: computer({ view: "composer", blocked: "Chat isn't available" }).host });
    await app.waitFor("text:Chat isn't available");
    expect(app.enabled("id:send")).toBe(false);
  }, 20_000);
});
