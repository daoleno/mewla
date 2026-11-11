import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const session = { id: "w1", name: "notes", status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() };
const transcript = [
  { id: "u1", seq: 1, kind: "user_message", timestamp: "2026-11-11T07:00:00.000Z", body: "Delete the build folder" },
  { id: "a1", seq: 2, kind: "assistant_message", timestamp: "2026-11-11T07:00:05.000Z", body: "Done." },
];
const daemon = { brain: {}, worker_sessions: [session], conversations: { w1: transcript } };

/** A desktop browser opened on the Session's URL. */
function desk(initialPath = "/sessions/w1") {
  const secure = new Map<string, string>();
  return {
    platform: "web" as const,
    width: 1280,
    height: 800,
    storage: paired,
    host: {
      params: { daemon },
      handlers: {
        "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
        "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
        "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
        "app.legacyStorage": null,
        "app.legacyStorageCopied": null,
        "app.origin": "http://127.0.0.1:1",
        "links.initial": null,
        "app.testFrame": null,
        "web.initialPath": { path: initialPath, state: null },
        "web.setLocation": null,
      },
    },
  };
}

const sent = (app: { host: { posts(): { name: string; data: unknown }[] } }) => framesOf(app.host.posts(), "send_input");

describe("the composer's keys on a desktop", () => {
  // Return as the field's `submitBehavior` says (`app.submit`). Shift/Alt+Return's new line, Ctrl/⌘+Return
  // and the IME guard are the browser runtime's (nsh.js wireInput), which the headless keyboard does not model.
  test("Return sends and empties the box", async () => {
    const app = await open(APP, desk());
    await app.waitFor("text:Delete the build folder", { timeout: 10_000 });
    await app.type("id:composer", "first");
    await app.submit("id:composer");
    await app.waitUntil(() => sent(app).length === 1);
    expect(sent(app)[0]).toMatchObject({ worker_id: "w1", text: "first" });
    expect(app.value("id:composer")).toBe("");
  }, 20_000);

  test("Return while a typed / list is open waits for a pick", async () => {
    const codex = { ...session, provider: "codex", command: "codex", capabilities: { structured_events: true } };
    const commands = [{ value: "/status", name: "status", description: "Show status", category: "session", chat_supported: true }];
    const base = desk();
    const app = await open(APP, { ...base, host: { ...base.host, params: { daemon: { ...daemon, worker_sessions: [codex], replies: { codex_slash_commands: { type: "codex_slash_commands", commands } } } } } });
    await app.waitFor("text:Delete the build folder", { timeout: 10_000 });
    await app.type("id:composer", "/sta");
    await app.waitFor("id:slash-status");
    await app.submit("id:composer");
    await app.wait(50);
    expect(sent(app)).toHaveLength(0);
    expect(app.value("id:composer")).toBe("/sta");
  }, 20_000);

  test("↑ in the empty composer edits the last message", async () => {
    const app = await open(APP, desk());
    await app.waitFor("text:Delete the build folder", { timeout: 10_000 });
    await app.type("id:composer", "x");
    await app.clear("id:composer");
    await app.keyboard.press("up");
    await app.waitFor("id:edit-chip");
    expect(app.value("id:composer")).toBe("Delete the build folder");
  }, 20_000);

  test("/ on the page goes to the message box; Escape leaves it", async () => {
    const app = await open(APP, desk("/"));
    // Brain's page is up once the computer said where Brain is; `?` opening the cheat sheet
    // says the page's keys are bound (a slow runner binds them after the first render).
    await app.waitFor("text:Brain is starting…");
    await app.keyboard.press("shift+/");
    await app.waitFor("id:shortcuts");
    await app.keyboard.press("escape");
    await app.waitUntil(() => !app.shows("id:shortcuts"));
    await app.keyboard.press("/");
    // In the field, `N` is a letter, not New session.
    await app.keyboard.press("n");
    await app.waitUntil(() => app.value("id:composer") === "n");
    expect(app.shows("id:new-cwd")).toBe(false);
    await app.keyboard.press("escape");
    // Outside it again, `N` is the shortcut.
    await app.keyboard.press("n");
    await app.waitFor("id:new-cwd");
  }, 20_000);
});

describe("the composer on a phone", () => {
  test("↑ from a hardware keyboard in the empty composer edits the last message", async () => {
    const app = await open(APP, { host: phoneHost(daemon), storage: paired, platform: "android" });
    await app.host.send("links.open", { url: "mewla://sessions" });
    await app.waitFor("text:notes", { timeout: 10_000 });
    await app.press("text:notes");
    await app.waitFor("text:Delete the build folder");
    await app.type("id:composer", "x");
    await app.clear("id:composer");
    await app.keyboard.press("up");
    await app.waitFor("id:edit-chip");
    expect(app.value("id:composer")).toBe("Delete the build folder");
  }, 20_000);

  test("Return is a new line; Send sends", async () => {
    const app = await open(APP, { host: phoneHost(daemon), storage: paired, platform: "android" });
    await app.host.send("links.open", { url: "mewla://sessions" });
    await app.waitFor("text:notes", { timeout: 10_000 });
    await app.press("text:notes");
    await app.waitFor("id:composer");
    await app.type("id:composer", "hello");
    await app.submit("id:composer");
    await app.wait(50);
    expect(sent(app)).toHaveLength(0);
    expect(app.value("id:composer")).toBe("hello\n");
    await app.press("id:send");
    await app.waitUntil(() => sent(app).length === 1);
  }, 20_000);
});
