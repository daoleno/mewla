import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, framesOf, paired, phoneHost } from "../testing/fixtures";
import manifest from "../nsh.json";
import notes from "./notes.json";
import { knowsScene } from "./scenes";

/**
 * What's New (Brain's SPEC 11-16): after an update, the first quiet moment on Brain home shows the sheet once; every
 * update leaves a line in Brain's chat. The fake computer keeps the record as the daemon does (daemon/whatsnew). This
 * app is at the release's version (nsh.json, APP_VERSION); the computer says it runs 0.4.9, so 0.4.9's notes count.
 */

const APP = "../FakeDaemonApp.tsx";
/** The app's own version: release preparation moves it, so no test names it. */
const APP_VERSION: string = manifest.version;
const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
const DAEMON_UPDATE = "GET http://127.0.0.1:1/daemon-update";
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-16T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const at = (minute: number) => Date.UTC(2026, 10, 16, 7, minute);
const say = (i: number, body: string, minute: number) => ({ id: `m${i}`, seq: i, kind: i % 2 ? "assistant_message" : "user_message", role: i % 2 ? "assistant" : "user", timestamp: new Date(at(minute)).toISOString(), body });
/** This device already ran this app: no fresh install, and no update of its own. */
const ranBefore = { "mewla:whats_new": JSON.stringify({ app: APP_VERSION }) };

type Kept = { baseline: string; seen?: string[]; updates?: { version: string; at: number }[]; replay?: boolean; releases?: unknown };

function setup(options: { record?: Kept; computer?: string; storage?: { [key: string]: string }; platform?: "android" | "web"; paired?: boolean; conversation?: unknown[] } = {}) {
  const daemon = {
    brain,
    conversations: { "brain-host": options.conversation ?? [say(0, "Morning", 1), say(1, "Morning! All quiet.", 2)] },
    ...(options.record ? { whats_new: options.record } : {}),
  };
  return {
    platform: options.platform ?? ("android" as const),
    // A manual clock: the quiet moment and the waits after it pass at once.
    clock: {},
    storage: { ...(options.paired === false ? {} : paired), ...ranBefore, ...options.storage },
    network: { ...expoTokenService, [DAEMON_UPDATE]: json({ version: options.computer ?? "0.4.9", supported: true }) },
    host: phoneHost(daemon),
  };
}

describe("What's New: when it shows", () => {
  test("after an update a quiet Brain home shows one sheet, newest first, at most three pages, and only once", async () => {
    const record: Kept = { baseline: "0.4.6" };
    const app = await open(APP, setup({ record }));
    await app.waitFor("id:whats-new-page-0", { timeout: 15_000 });
    // Three releases were missed: each one's headline, newest first.
    expect(app.shows("text:Diagrams and files, right in Chat")).toBe(true);
    expect(app.count("id:whats-new-dot-2")).toBe(1);
    expect(app.shows("id:whats-new-dot-3")).toBe(false);
    expect(app.shows("text:Mewla 0.4.9")).toBe(true);
    // Shown is seen, on the computer: the phone and the web share it.
    await app.waitUntil(() => framesOf(app.host.posts(), "whats_new_seen").length === 1);
    // The sheet settles the three releases: the highlights it had no room for are not offered after it.
    const settled = framesOf(app.host.posts(), "whats_new_seen")[0].ids as string[];
    expect(settled.slice(0, 3)).toEqual(["0.4.9-diagrams-and-files-in-chat", "0.4.8-browser-survives", "0.4.7-agent-questions-in-chat"]);
    expect(settled.length).toBe(7);
    // The last page has Done; it closes and does not come back.
    await app.press("id:whats-new-dot-2");
    await app.waitFor("id:whats-new-done");
    await app.press("id:whats-new-done");
    await app.waitUntil(() => !app.shows("id:whats-new"));
    await app.wait(2_500);
    expect(app.shows("id:whats-new")).toBe(false);
  }, 40_000);

  test("a release with fixes only shows no sheet, only its line in Brain's chat, which lists the fixes", async () => {
    const release = { version: "0.5.0", date: "2026-11-20", highlights: [], fixes: ["Sessions open faster."] };
    // The baseline is past every bundled release, so only this one is news.
    const record: Kept = { baseline: "0.4.16", updates: [{ version: "0.5.0", at: at(3) }], releases: { releases: [release] } };
    const app = await open(APP, setup({ record, computer: "0.5.0" }));
    await app.waitFor("id:update-line", { timeout: 15_000 });
    expect(app.shows("text:Updated to 0.5.0 ·")).toBe(true);
    await app.wait(2_500);
    expect(app.shows("id:whats-new")).toBe(false);
    await app.press("id:update-line");
    await app.waitFor("id:whats-new-fixes");
    expect(app.shows("text:Sessions open faster.")).toBe(true);
  }, 40_000);

  test("opened from a link, it waits: the person came for that; back on Brain on their own, it shows", async () => {
    const app = await open(APP, setup({ record: { baseline: "0.4.8" } }));
    await app.waitFor("id:composer", { timeout: 15_000 });
    await app.host.send("links.open", { url: "mewla://calendar?serverId=s1" });
    await app.wait(3_000);
    expect(app.shows("id:whats-new")).toBe(false);
    await app.press("id:page-back");
    await app.waitFor("id:whats-new-page-0", { timeout: 10_000 });
  }, 40_000);

  test("seen on another device (the web) is not shown here", async () => {
    const seen = ["0.4.9-diagrams-and-files-in-chat", "0.4.9-back-after-network-switch", "0.4.9-updates-in-chat"];
    const app = await open(APP, setup({ record: { baseline: "0.4.8", seen } }));
    await app.waitFor("id:composer", { timeout: 15_000 });
    await app.wait(3_000);
    expect(app.shows("id:whats-new")).toBe(false);
  }, 30_000);

  test("on the web it is a card, and what the phone saw stays seen", async () => {
    const record: Kept = { baseline: "0.4.8" };
    const app = await open(APP, setup({ record, platform: "web" }));
    await app.waitFor("id:whats-new-page-0", { timeout: 15_000 });
    // No swipe on the web: arrows move the pages.
    await app.press("id:whats-new-next");
    await app.waitFor("id:whats-new-page-1");
    expect(app.shows("text:Back in seconds after switching networks")).toBe(true);
    expect(framesOf(app.host.posts(), "whats_new_seen")[0].ids).toEqual(["0.4.9-diagrams-and-files-in-chat", "0.4.9-back-after-network-switch", "0.4.9-updates-in-chat"]);
  }, 30_000);

  test("never over a draft: the sheet waits while Brain's composer holds text", async () => {
    const app = await open(APP, setup({ record: { baseline: "0.4.8" } }));
    await app.waitFor("id:composer", { timeout: 15_000 });
    await app.type("id:composer", "Draft");
    await app.wait(3_000);
    expect(app.shows("id:whats-new")).toBe(false);
  }, 30_000);

  test("never over a card that needs the person: an approval in Brain's thread holds it back", async () => {
    const approval = { id: "a1", tool: "Bash", summary: "git push origin main", options: [{ id: "allow", label: "Allow", scope: "once" }, { id: "deny", label: "Deny", scope: "deny" }] };
    const docs = { id: "docs", name: "docs", status: "blocked", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now(), delegated: true, approvals: [approval] };
    const options = setup({ record: { baseline: "0.4.8" } });
    const daemon = (options.host.params as { daemon: Record<string, unknown> }).daemon;
    const app = await open(APP, { ...options, host: { ...options.host, params: { daemon: { ...daemon, worker_sessions: [docs] } } } });
    await app.waitFor("id:request-card", { timeout: 15_000 });
    await app.wait(3_000);
    expect(app.shows("id:whats-new")).toBe(false);
  }, 30_000);

  test("a fresh install shows nothing and keeps its own start: onboarding is first", async () => {
    const app = await open(APP, { ...setup({ record: { baseline: "0.4.6" }, paired: false }), storage: {} });
    await app.wait(3_000);
    expect(app.shows("id:whats-new")).toBe(false);
    expect(JSON.parse(app.storage()["mewla:whats_new"])).toEqual({ app: APP_VERSION, since: APP_VERSION });
  }, 30_000);

  test("the app's own update is told to the computer once, for its chat line", async () => {
    const record: Kept = { baseline: "0.4.6", seen: [] };
    const app = await open(APP, setup({ record, storage: { "mewla:whats_new": JSON.stringify({ app: "0.4.7" }) } }));
    await app.waitUntil(() => framesOf(app.host.posts(), "whats_new_note").length === 1, { timeout: 15_000 });
    expect(framesOf(app.host.posts(), "whats_new_note")[0].version).toBe(APP_VERSION);
    expect(JSON.parse(app.storage()["mewla:whats_new"]).app).toBe(APP_VERSION);
  }, 30_000);
});

describe("What's New: Try it", () => {
  test("Try it closes the sheet and opens the place", async () => {
    const app = await open(APP, setup({ record: { baseline: "0.4.8" } }));
    await app.waitFor("id:whats-new-try", { timeout: 15_000 });
    // 0.4.9's second page: the network, on the computer page.
    await app.press("id:whats-new-dot-1");
    await app.waitFor("id:whats-new-page-1");
    await app.wait(500);
    await app.press("id:whats-new-try");
    await app.waitFor("id:computer-page");
    expect(app.shows("id:whats-new")).toBe(false);
  }, 30_000);

  test("About's What's new reopens the latest notes any time", async () => {
    // Its own notes, not the latest real ones: those change with every release and may be fixes only.
    const release = {
      version: "0.5.0", date: "2026-11-20", fixes: [],
      highlights: [{ id: "0.5.0-test-page", title: "A test page", line: "Shown again from About.", media: { scene: "chat-diagram" }, try: { kind: "open", target: "brain" } }],
    };
    const record: Kept = { baseline: "0.4.9", seen: ["0.5.0-test-page"], updates: [{ version: "0.5.0", at: at(3) }], releases: { releases: [release] } };
    const app = await open(APP, setup({ record, computer: "0.5.0" }));
    // The computer's notes and version are in once its update line is.
    await app.waitFor("id:update-line", { timeout: 15_000 });
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    await app.press("id:computer-settings");
    await app.waitFor("id:settings-whats-new");
    await app.press("id:settings-whats-new");
    await app.waitFor("id:whats-new-page-0");
    expect(app.shows("text:A test page")).toBe(true);
  }, 30_000);
});

test("every bundled page draws a scene this app knows", () => {
  for (const release of (notes as { releases: { highlights: { media: { scene: string } }[] }[] }).releases) {
    for (const highlight of release.highlights) expect(knowsScene(highlight.media.scene)).toBe(true);
  }
});
