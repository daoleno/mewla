import { describe, expect, open, test } from "nsh/test";
import { answering } from "../testing/fixtures";

// Brain's own things as components (soft-final/SPEC.md §1.6), driven on their own: the
// ⋯ menu and its pages ("Brain host", Model).
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
  test("five rows in order, Model showing the model and effort; a row it cannot do stays at its place and does nothing", async () => {
    const c = computer({ threadLoaded: false });
    const app = await menu(c);
    expect(app.texts().filter((text) => ["Open terminal", "Browse workspace", "Switch executor", "Model", "New chat"].includes(text)))
      .toEqual(["Open terminal", "Browse workspace", "Switch executor", "Model", "New chat"]);
    await app.waitFor("text:claude-sonnet-5-5 · High");
    // Brain's thread has not loaded: New chat is there, not enabled.
    expect(app.enabled("id:brain-menu-new-chat")).toBe(false);
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
    expect(app.texts()).toContain("Claude Code · tmux · me@x");
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

  test("Model opens the Model & Effect panel for Brain's own Session, in the same sheet", async () => {
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
