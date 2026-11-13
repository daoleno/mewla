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

  test("a terminal Session's collapsed capsule has its mark bare, in its own box, not the terminal's tile (owner 11-13: the tile sat on the name)", async () => {
    const shell = { id: "t1", name: "mewla-nsh-client", status: "running", cwd: "/home/me/mewla-nsh-client", command: "zsh", updated_at: Date.now() };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [shell], conversations: { "brain-host": [] } }) });
    await app.host.send("links.open", { url: "mewla://sessions/t1" });
    await app.waitFor("id:capsule-collapsed", { timeout: 10_000 });
    expect(app.shows("id:session-status")).toBe(true);
    expect(app.count("id:capsule-collapsed >> id:agent-tile")).toBe(0);
    await app.press("id:capsule-name");
    await app.waitFor("id:capsule");
    expect(app.count("id:capsule >> id:agent-tile")).toBe(0);
  }, 30_000);
});

describe("Brain's capsule header (SPEC §11)", () => {
  const busy = { id: "w2", name: "api", status: "running", cwd: "/home/me/api", command: "claude", provider: "claude", started_at: 1, updated_at: Date.now() };
  async function brainChat() {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [busy], conversations: { "brain-host": thread } }) });
    await app.waitFor("id:brain-chat", { timeout: 10_000 });
    await app.waitFor("text:1 running", { timeout: 10_000 });
    await app.wait(300);
    return app;
  }

  test("collapses to the cat and the short state while the person reads; both still act; ⋯ returns at the newest end", async () => {
    const app = await brainChat();
    expect(app.shows("id:capsule")).toBe(true);
    expect(app.shows("id:home-title")).toBe(true);
    expect(app.shows("id:brain-menu")).toBe(true);
    await app.scroll("id:brain-chat", -300);
    await app.waitFor("id:capsule-collapsed");
    // The cat and the state control stay, the state short ("● 1"); "Brain" and ⋯ are gone.
    expect(app.shows("id:home-cat")).toBe(true);
    expect(app.texts()).toContain("1");
    expect(app.shows("text:1 running")).toBe(false);
    expect(app.shows("id:home-title")).toBe(false);
    expect(app.shows("id:brain-menu")).toBe(false);
    // The state control still opens the tray at once: no tap that only expands.
    await app.press("id:state-control");
    await app.waitFor("id:tray-row");
    await app.back();
    await app.waitUntil(() => !app.shows("id:tray-row"));
    // At the newest end it expands, ⋯ with it.
    await app.scroll("id:brain-chat", 2000);
    await app.waitFor("id:capsule");
    expect(app.shows("id:brain-menu")).toBe(true);
    expect(app.shows("text:1 running")).toBe(true);
  }, 30_000);

  test("one more thing that needs the person brings it back", async () => {
    const app = await brainChat();
    await app.scroll("id:brain-chat", -600);
    await app.waitFor("id:capsule-collapsed");
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...busy, status: "blocked", process_id: 42, started_at: 1_700_000_000_000, prompt: { id: "p-1", kind: "permission", provider: "claude", title: "Run this command?", request: "rm -rf build", options: [{ number: 1, label: "Yes", selected: true }, { number: 2, label: "No" }] } } });
    await app.waitFor("id:capsule");
  }, 30_000);
});

describe("a pushed page's capsule (SPEC §11)", () => {
  test("Back alone while the page's own title shows; past 50 of scroll the title joins it, and it never collapses", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain }) });
    await app.waitFor("id:home-cat", { timeout: 10_000 });
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    await app.press("id:computer-settings");
    await app.waitFor("id:settings");
    await app.waitFor("id:capsule-collapsed");
    expect(app.shows("id:page-back")).toBe(true);
    expect(app.shows("id:page-title")).toBe(false);
    await app.scroll("id:settings", 300);
    await app.waitFor("id:page-title");
    expect(app.shows("id:capsule")).toBe(true);
    expect(app.texts()).toContain("Settings");
    // Back to its top, the title is the page's own again.
    await app.scroll("id:settings", -300);
    await app.waitFor("id:capsule-collapsed");
    // Back pops the page.
    await app.press("id:page-back");
    await app.waitFor("id:computer-page");
  }, 30_000);
});

describe("the canvas under the capsule (owner 11-13 14:45: a dark band behind it on a light page)", () => {
  // The screen view behind a page is painted in the system's theme: Mewla light on a phone set to dark showed
  // it wherever the page did not cover the top. Every screen with a capsule stands on the app's canvas
  // (`CapsulePage`, bg-background), whatever the system's theme. Headless NSH reads no view's colour, so this
  // holds the structure: the capsule is inside the canvas root, on each kind of page, in both cases.
  for (const [app, system] of [["light", "dark"], ["dark", "light"]] as const) {
    test(`Mewla ${app} on a system set to ${system}: Brain, a Session, a pushed page and a page with its own bar all stand on the canvas`, async () => {
      const a = await open(APP, { platform: "android", colorScheme: system, storage: { ...paired, "mewla:theme_preference": `classic-${app}` }, host: phoneHost({ brain, worker_sessions: [session], conversations: { "brain-host": [], w1: thread } }) });
      await a.waitFor("id:home-header", { timeout: 10_000 });
      expect(a.shows("id:capsule-page")).toBe(true);
      // A pushed pattern page (kit Page, its content scrolls under the capsule).
      await a.press("id:home-cat");
      await a.waitFor("id:computer-page");
      expect(a.shows("id:capsule-page")).toBe(true);
      await a.back();
      // A page on NavPage itself, with its own display title (All Sessions).
      await a.press("id:state-control");
      await a.waitFor("id:tray-all-sessions");
      await a.press("id:tray-all-sessions");
      await a.waitFor("id:page-back");
      expect(a.shows("id:capsule-page")).toBe(true);
      // A Session.
      await a.host.send("links.open", { url: "mewla://sessions/w1" });
      await a.waitFor("id:session-chat", { timeout: 10_000 });
      expect(a.shows("id:capsule-page")).toBe(true);
    }, 30_000);
  }
});
