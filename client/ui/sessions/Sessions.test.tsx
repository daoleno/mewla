import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const session = (id: string, patch: Record<string, unknown> = {}) => ({ id, name: "", status: "done", cwd: "/home/me/mewla", command: "claude --permission-mode bypassPermissions", provider: "claude", updated_at: Date.now(), ...patch });

async function sessionsApp(daemon: Record<string, unknown>) {
  const app = await open(APP, { host: phoneHost({ brain: {}, ...daemon }), storage: paired, platform: "android" });
  await app.host.send("links.open", { url: "mewla://sessions" });
  return app;
}

describe("Sessions", () => {
  test("lists the computer's Sessions by folder, a caption per folder once there are two", async () => {
    const app = await sessionsApp({ worker_sessions: [session("a", { name: "release notes" }), session("b", { cwd: "/home/me/site", status: "running", provider: "codex", command: "codex" }), session("c", { needs_attention: true, status: "blocked" })] });
    await app.waitFor("text:release notes");
    expect(app.count("id:session-row")).toBe(3);
    expect(app.texts()).toContain("mewla");
    expect(app.texts()).toContain("site");
    // The first caption is the mewla folder's: folded, it keeps its count and its Session waiting on you.
    await app.press("id:section >> nth=0");
    await app.waitFor("text:2");
    expect(app.count("id:session-row")).toBe(1);
  }, 20_000);

  test("one folder has no caption; with none, it offers New session", async () => {
    const one = await sessionsApp({ worker_sessions: [session("a", { name: "only" })] });
    await one.waitFor("text:only");
    expect(one.count("id:section")).toBe(0);
    const none = await sessionsApp({ worker_sessions: [] });
    await none.waitFor("text:No sessions yet");
    expect(none.texts()).toContain("New session");
  }, 20_000);

  test("New session starts the agent in the folder shown and opens it", async () => {
    const app = await sessionsApp({ worker_sessions: [session("a", { name: "first" })] });
    await app.waitFor("text:first");
    await app.press("id:new-session");
    await app.waitFor("id:preset-codex");
    await app.press("id:preset-codex");
    await app.waitFor("id:session-menu");
    const create = framesOf(app.host.posts(), "create_session").at(-1)!;
    expect(create).toMatchObject({ cwd: "/home/me/mewla", command: "codex --dangerously-bypass-approvals-and-sandbox", name: "" });
    const active = framesOf(app.host.posts(), "set_active_worker").map((f) => f.worker_id);
    expect(active.at(-1)).toBe("w101");
  }, 20_000);

  test("a long press selects; Terminate asks, then ends them and they leave the list", async () => {
    const app = await sessionsApp({ worker_sessions: [session("a", { name: "one" }), session("b", { name: "two" })] });
    await app.waitFor("text:one");
    await app.longPress("text:one");
    await app.waitFor("text:1 selected");
    await app.press("text:two");
    await app.waitFor("text:2 selected");
    app.onDialog(() => "Terminate");
    await app.press("id:selection-terminate");
    await app.waitFor("text:No sessions yet");
    expect(framesOf(app.host.posts(), "kill_worker").map((f) => f.worker_id).sort()).toEqual(["a", "b"]);
    expect(app.dialogs()[0]).toMatchObject({ title: "Terminate sessions?" });
  }, 20_000);
});

describe("a Session", () => {
  const transcript = [
    { id: "u1", seq: 1, kind: "user_message", timestamp: "2026-11-11T07:00:00.000Z", body: "Delete the build folder" },
    { id: "a1", seq: 2, kind: "assistant_message", timestamp: "2026-11-11T07:00:05.000Z", body: "I need your **permission** to run `rm -rf build`." },
  ];
  const blocked = session("w9", {
    name: "cleanup", status: "blocked", process_id: 42, started_at: 1_700_000_000_000,
    prompt: { id: "p-1", kind: "permission", provider: "claude", title: "Run this command?", request: "rm -rf build", options: [{ number: 1, label: "Yes", selected: true }, { number: 2, label: "No" }] },
  });

  test("shows its chat, and the prompt open in its terminal as a card the person can answer", async () => {
    const app = await sessionsApp({ worker_sessions: [blocked], conversations: { w9: transcript } });
    await app.waitFor("text:cleanup");
    await app.press("text:cleanup");
    await app.waitFor("id:action-prompt");
    expect(app.texts()).toContain("Run this command?");
    expect(app.texts()).toContain("Delete the build folder");
    await app.press("id:prompt-option-2");
    await app.waitFor("text:Running");
    expect(framesOf(app.host.posts(), "answer_prompt").at(-1)).toMatchObject({ worker_id: "w9", prompt_answer: { prompt_id: "p-1", number: 2 } });
    expect(app.shows("id:action-prompt")).toBe(false);
  }, 20_000);

  test("the menu switches to the terminal and renames it on this phone", async () => {
    const app = await sessionsApp({ worker_sessions: [session("w1", { name: "notes", status: "done" })], conversations: { w1: transcript } });
    await app.press("text:notes");
    await app.waitFor("id:session-chat");
    app.onDialog((dialog) => (dialog.buttons.includes("Rename") ? "Rename" : null));
    await app.press("id:session-menu");
    await app.waitFor("id:rename-input");
    await app.type("id:rename-input", "Scratchpad");
    await app.press("id:rename-save");
    await app.waitFor("text:Scratchpad");
    await app.back();
    await app.waitFor("text:Scratchpad");
  }, 20_000);

  test("a DSH Session's permission request is reviewed and allowed once", async () => {
    const dshSession = session("w5", { name: "dsh run", status: "running", provider: "dsh", command: "dsh", process_id: 77, started_at: 1_700_000_000_000, capabilities: { structured_events: true } });
    const app = await sessionsApp({
      worker_sessions: [dshSession],
      conversations: { w5: { source: "dsh_session_jsonl", events: [] } },
      dsh: { epoch: "e1", items: [{ rpcId: "r1", payload: { type: "approval/requested", sessionId: "s", toolName: "Bash", reason: "Run `make deploy`" } }] },
    });
    await app.press("text:dsh run");
    await app.waitFor("id:dsh-banner");
    expect(app.texts()).toContain("Permission requested");
    await app.press("id:dsh-review");
    await app.waitFor("id:dsh-allow");
    await app.press("id:dsh-allow");
    await app.waitFor("id:dsh-banner", { state: "gone" });
    const answers = framesOf(app.host.posts(), "dsh_interaction").filter((f) => f.dsh_answer);
    expect(answers.at(-1)).toMatchObject({ worker_id: "w5", process_id: 77, dsh_answer: { epoch: "e1", rpcId: "r1", outcome: "allowed-once" } });
  }, 20_000);

  test("a mewla://sessions/<id> link (a notification, Work, Calendar) opens that Session", async () => {
    const app = await sessionsApp({ worker_sessions: [session("w7", { name: "linked" })], conversations: { w7: [] } });
    await app.waitFor("text:linked");
    await app.host.send("links.open", { url: "mewla://brain" });
    await app.host.send("links.open", { url: "mewla://sessions/w7" });
    await app.waitFor("id:session-menu");
    expect(framesOf(app.host.posts(), "set_active_worker").at(-1)?.worker_id).toBe("w7");
  }, 20_000);
});
