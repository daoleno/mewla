import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const brain = { chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" } };
const at = (i: number) => new Date(Date.UTC(2026, 10, 13, 1, 0, i)).toISOString();
const words = "A long line of words that wraps over several rows in a phone-sized chat. ".repeat(4);
const thread = [
  ...Array.from({ length: 12 }, (_, i) => ({ id: `m${i}`, seq: i + 1, kind: i % 2 ? "assistant_message" : "user_message", timestamp: at(i), body: `Message ${i}. ${words}` })),
  { id: "p", seq: 13, kind: "assistant_message", timestamp: at(13), body: "Here is the chart:\n\n![chart](out/chart.png)" },
];
const session = { id: "w1", name: "mewla", status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() };
// A picture that does not load is asked for again each time its row is made: the count of those asks is how
// often the newest row was drawn.
const noPicture = { codex_asset: { type: "error", message: "not a picture" } };
const builds = (app: { host: { posts(): { name: string; data: unknown }[] } }) => framesOf(app.host.posts(), "codex_asset").length;

async function sessionChat() {
  const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [session], conversations: { "brain-host": [], w1: thread }, replies: noPicture }) });
  await app.host.send("links.open", { url: "mewla://sessions/w1" });
  await app.waitFor("id:session-chat", { timeout: 10_000 });
  await app.waitUntil(() => builds(app) >= 1, { timeout: 10_000 });
  await app.wait(300);
  return app;
}

describe("a Session's capsule header (SPEC §11)", () => {
  test("collapses while the person reads, without drawing a row again; a tap on its name or the newest end brings it back", async () => {
    const app = await sessionChat();
    expect(app.shows("id:capsule")).toBe(true);
    expect(app.shows("id:session-menu")).toBe(true);
    const once = builds(app);
    await app.scroll("id:session-chat", -300);
    await app.waitFor("id:capsule-collapsed");
    // Collapsed, it keeps Back, the mark and the name: the view button and ⋯ are gone.
    expect(app.shows("id:page-back")).toBe(true);
    expect(app.shows("id:session-status")).toBe(true);
    expect(app.shows("id:session-mode")).toBe(false);
    expect(app.shows("id:session-menu")).toBe(false);
    // A tap on the name brings it back.
    await app.press("id:capsule-name");
    await app.waitFor("id:capsule");
    // Collapsed again, then back at the newest end: it expands.
    await app.scroll("id:session-chat", -300);
    await app.waitFor("id:capsule-collapsed");
    await app.scroll("id:session-chat", 800);
    await app.waitFor("id:capsule");
    // The thread was never drawn again for any of it.
    expect(builds(app)).toBe(once);
  }, 30_000);

  test("a message arriving while the person reads does not bring it back; something that needs them does", async () => {
    const app = await sessionChat();
    await app.scroll("id:session-chat", -600);
    await app.waitFor("id:capsule-collapsed");
    await app.host.send("app.conversation", { worker: "w1", upserts: [{ id: "n1", seq: 20, kind: "assistant_message", timestamp: at(20), body: "One more thing." }] });
    await app.wait(400);
    expect(app.shows("id:capsule-collapsed")).toBe(true);
    // The Session stops on a prompt only its terminal can answer: it goes there, where the capsule is collapsed,
    // and its mark has the red dot.
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...session, status: "blocked" } });
    await app.waitFor("id:capsule-needs-you");
  }, 30_000);

  test("a new request in the chat brings it back", async () => {
    const app = await sessionChat();
    await app.scroll("id:session-chat", -600);
    await app.waitFor("id:capsule-collapsed");
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...session, status: "blocked", process_id: 42, started_at: 1_700_000_000_000, prompt: { id: "p-1", kind: "permission", provider: "claude", title: "Run this command?", request: "rm -rf build", options: [{ number: 1, label: "Yes", selected: true }, { number: 2, label: "No" }] } } });
    await app.waitFor("id:capsule");
    expect(app.shows("id:capsule-needs-you")).toBe(true);
  }, 30_000);

  test("its terminal has it collapsed: a tap on the name brings it over the top rows, a touch in the terminal folds it", async () => {
    const app = await sessionChat();
    await app.press("id:session-mode");
    await app.waitUntil(() => !app.shows("id:session-chat"));
    await app.waitFor("id:capsule-collapsed");
    await app.press("id:capsule-name");
    await app.waitFor("id:capsule");
    // A touch in the terminal (its key bar here) folds it again. (It is seen on the way in, not taken; the fake
    // computer attaches no terminal, so the key's own frame is not checked here.)
    await app.press("text:esc");
    await app.waitFor("id:capsule-collapsed");
    await app.press("id:capsule-name");
    await app.waitFor("id:capsule");
    expect(app.shows("label:Show chat")).toBe(true);
    // Back to the chat: it opens expanded.
    await app.press("id:session-mode");
    await app.waitFor("id:session-chat");
    expect(app.shows("id:capsule")).toBe(true);
  }, 30_000);
});
