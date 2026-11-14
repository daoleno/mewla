import { describe, expect, open, test } from "nsh/test";
import { answering, framesOf, paired, phoneHost, pressUntil } from "../testing/fixtures";

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
// The daemon's answer for a file it cannot show as a picture (a read that did not answer would be asked again).
const noPicture = { codex_asset: { type: "error", code: "codex_asset_failed", message: "asset is not a supported image" } };
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

  test("a delegated Worker stopped on its own report gets no dot: that is Brain's to answer (owner 2026-11-13, shot 11)", async () => {
    const app = await sessionChat();
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...session, status: "blocked", delegated: true, needs_attention: true, attention: "user_input", summary: "Should I also purge the cache?" } });
    await app.wait(500);
    expect(app.shows("id:capsule-needs-you")).toBe(false);
    // A prompt on its terminal Brain cannot answer is the person's.
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...session, status: "blocked", delegated: true, process_id: 42, started_at: 1_700_000_000_000, prompt: { id: "p-2", kind: "permission", provider: "claude", title: "Sign in?", request: "gh auth login", options: [{ number: 1, label: "Yes", selected: true }, { number: 2, label: "No" }] } } });
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

  // Owner 2026-11-14: Git is the capsule's own button beside the view button, only for a Session in a repository.
  const repoStatus = { available: true, repo_root: "/home/me/mewla", repo_name: "mewla", branch: "main", has_commits: true, clean: false, file_count: 1, staged_file_count: 0, unstaged_file_count: 1, untracked_file_count: 0, additions: 3, deletions: 1, files: [{ path: "app.ts", staged: false, unstaged: true, untracked: false, additions: 3, deletions: 1 }] };
  async function gitSession(status: Record<string, unknown>) {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [session], conversations: { "brain-host": [], w1: thread }, replies: { ...noPicture, git_diff_status: { type: "git_diff_status", status } } }) });
    await app.host.send("links.open", { url: "mewla://sessions/w1" });
    await app.waitFor("id:session-chat", { timeout: 10_000 });
    await app.waitUntil(() => framesOf(app.host.posts(), "git_diff_status").length > 0, { timeout: 10_000 });
    return app;
  }

  test("a Session in a Git repository has a Git button beside the view button: it fades with it, and it opens the Session's Git review; ⋯ no longer lists Changes", async () => {
    const app = await gitSession(repoStatus);
    await app.waitFor("id:capsule >> id:session-git");
    expect(framesOf(app.host.posts(), "git_diff_status").at(-1)).toMatchObject({ target_id: "w1", cwd: "/home/me/mewla" });
    expect(app.shows("label:Git changes")).toBe(true);
    // Collapsed while the person reads, it goes with the view button and ⋯, and comes back with them.
    await app.scroll("id:session-chat", -300);
    await app.waitFor("id:capsule-collapsed");
    expect(app.shows("id:session-git")).toBe(false);
    await app.press("id:capsule-name");
    await app.waitFor("id:capsule");
    expect(app.shows("id:session-git")).toBe(true);
    expect(app.shows("id:session-mode")).toBe(true);
    // One door: ⋯ has no Changes.
    app.onDialog((dialog) => (dialog.buttons.includes("Rename") ? "Cancel" : null));
    await answering(app, () => app.press("id:session-menu"));
    expect(app.dialogs().at(-1)!.buttons).toContain("Rename");
    expect(app.dialogs().at(-1)!.buttons).not.toContain("Changes");
    await app.press("id:session-git");
    await app.waitFor("id:git-page");
    // Read once for the Session; the Git page reads it again as it opens.
    expect(framesOf(app.host.posts(), "git_diff_status").at(-1)).toMatchObject({ target_id: "w1", cwd: "/home/me/mewla" });
  }, 30_000);

  test("a Session whose folder is not a Git repository has no Git button, and the status is read once, not per render", async () => {
    const app = await gitSession({ available: false, reason: "not_git_repo", clean: true, file_count: 0, staged_file_count: 0, unstaged_file_count: 0, untracked_file_count: 0, additions: 0, deletions: 0, files: [] });
    await app.wait(500);
    expect(app.shows("id:capsule")).toBe(true);
    expect(app.shows("id:session-mode")).toBe(true);
    expect(app.shows("id:session-git")).toBe(false);
    // Reading (collapse, expand) and the Session's own updates ask nothing more.
    await app.scroll("id:session-chat", -300);
    await app.waitFor("id:capsule-collapsed");
    await app.press("id:capsule-name");
    await app.waitFor("id:capsule");
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...session, status: "running" } });
    await app.wait(300);
    expect(app.shows("id:session-git")).toBe(false);
    expect(framesOf(app.host.posts(), "git_diff_status")).toHaveLength(1);
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
      // Back home first: the computer page goes with its own transition.
      await a.waitUntil(() => !a.shows("id:computer-page"), { timeout: 10_000 });
      // A page on NavPage itself, with its own display title (All Sessions).
      await pressUntil(a, "id:state-control", "id:tray-all-sessions");
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
