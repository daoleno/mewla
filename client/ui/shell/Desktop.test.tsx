import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";

/** The page host of a browser the computer serves (client/ui/web/host.js), as a test plays it. */
function webHost(daemon: Record<string, unknown> = { brain: {} }, extra: Record<string, unknown> = {}) {
  const secure = new Map<string, string>();
  return {
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
      "web.initialPath": { path: "/", state: null },
      "web.setLocation": null,
      ...extra,
    },
  };
}

type Location = { path: string; title: string; mode: string; state: { index: number; key: string; serverId: string | null; snapshot: unknown } };
const locations = (app: { host: { calls(): { name: string; data: unknown }[] } }) =>
  app.host.calls().filter((call) => call.name === "web.setLocation").map((call) => call.data as Location);
const last = (app: Parameters<typeof locations>[0]) => locations(app).at(-1);

const desk = { platform: "web" as const, width: 1280, height: 800, storage: paired };

describe("the desktop layout", () => {
  test("a Plugins sign-in that came back opens Extensions", async () => {
    const app = await open(APP, { ...desk, host: webHost({ brain: {} }, { "web.initialPath": { path: "/plugins", state: null }, "links.pluginReturn": { connected: "n1", service: "notion" } }) });
    await app.waitUntil(() => last(app)?.path === "/extensions", { timeout: 10_000 });
  }, 20_000);

  test("a mewla://settings link opens the Settings page it names", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain: {} }) });
    await app.waitFor("text:Brain is starting…", { timeout: 10_000 });
    await app.host.send("links.open", { url: "mewla://settings/computer?id=s1" });
    await app.waitFor("id:computer");
  }, 20_000);

  test("a right-click on a Session row starts selecting, as a long press does on a phone (parity #28)", async () => {
    const sessions = [{ id: "w1", name: "release", status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() }];
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: sessions }) });
    await app.waitFor("id:sidebar-sessions", { timeout: 10_000 });
    await app.press("id:sidebar-sessions");
    await app.waitFor("text:release");
    await app.rightClick("id:session-row");
    await app.waitFor("text:1 selected");
  }, 20_000);

  test("a shared URL to a sub-page opens that page, and the page keeps its URL (parity #47)", async () => {
    const app = await open(APP, { ...desk, host: webHost({ brain: {} }, { "web.initialPath": { path: "/settings/computer?id=s1", state: null } }) });
    await app.waitFor("id:computer", { timeout: 10_000 });
    await app.waitUntil(() => last(app)?.path === "/settings/computer?id=s1", { timeout: 10_000 });
    await app.press("id:sidebar-stats");
    await app.waitUntil(() => last(app)?.path === "/stats");
  }, 20_000);

  test("a place that waits on you has a dot beside its name", async () => {
    const sessions = [{ id: "w1", name: "release", status: "blocked", needs_attention: true, cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() }];
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: sessions }) });
    await app.waitFor("id:sidebar-sessions-dot", { timeout: 10_000 });
    expect(app.shows("id:sidebar-brain-dot")).toBe(false);
  }, 20_000);

  test("a sidebar of every place beside the page, Friends off the web; the place you pick has a URL and a tab title", async () => {
    const app = await open(APP, { ...desk, host: webHost() });
    await app.waitFor("id:sidebar");
    for (const key of ["brain", "sessions", "calendar", "extensions", "screen", "stats", "resources", "settings"]) expect(app.shows(`id:sidebar-${key}`)).toBe(true);
    expect(app.shows("id:sidebar-friends")).toBe(false);
    expect(app.count("id:tabs")).toBe(0);
    await app.waitUntil(() => locations(app).length > 0);
    expect(locations(app)[0]).toMatchObject({ path: "/", title: "Brain · Mewla", mode: "replace", state: { index: 0, serverId: "s1" } });

    const first = (path: string) => locations(app).find((location) => location.path === path);
    await app.press("id:sidebar-calendar");
    await app.waitUntil(() => last(app)?.path === "/calendar");
    expect(first("/calendar")).toMatchObject({ title: "Calendar · Mewla", mode: "push", state: { index: 1 } });

    await app.press("id:sidebar-settings");
    await app.waitFor("id:settings");
    await app.waitUntil(() => last(app)?.path === "/settings");
    expect(first("/settings")).toMatchObject({ title: "Settings · Mewla", mode: "push", state: { index: 2 } });
    // Nothing changed, nothing sent again: one write per place (browsers cap history writes).
    await app.wait(300);
    expect(locations(app).filter((location) => location.path === "/calendar")).toHaveLength(1);
    expect(locations(app).filter((location) => location.path === "/settings")).toHaveLength(1);
  }, 20_000);

  test("Back and Forward reopen what an entry showed; an entry without the app's state goes by its path", async () => {
    const app = await open(APP, { ...desk, host: webHost() });
    await app.waitUntil(() => locations(app).length > 0);
    await app.press("id:sidebar-stats");
    await app.waitFor("id:stats");
    await app.waitUntil(() => last(app)?.path === "/stats");
    const brain = locations(app)[0];
    // The browser's Back: the page host reports the entry it went to.
    await app.host.send("web.popstate", { path: brain.path, state: brain.state });
    await app.waitUntil(() => last(app)?.path === "/" && last(app)?.mode === "replace");
    // A path typed or bookmarked, with no state of the app's.
    await app.host.send("web.popstate", { path: "/resources", state: null });
    await app.waitFor("id:resources");
    await app.waitUntil(() => last(app)?.path === "/resources");
  }, 20_000);

  test("a reload opens the place its URL names, on its natural stack (Settings under the computer page, over home)", async () => {
    const app = await open(APP, { ...desk, host: webHost({ brain: {} }, { "web.initialPath": { path: "/settings", state: null } }) });
    await app.waitFor("id:settings");
    await app.waitUntil(() => locations(app).length > 0);
    expect(locations(app)[0]).toMatchObject({ path: "/settings", title: "Settings · Mewla", mode: "replace" });
    expect((locations(app)[0].state.snapshot as { routes: { name: string }[] }).routes.map((route) => route.name)).toEqual(["home", "computer", "settings"]);
  }, 20_000);

  test("a Session the daemon named after its id has the title its page shows, not the id twice", async () => {
    const id = "mh-c76c35d267c789a94b2d013b";
    const sessions = [{ id, name: `${id} (${id})`, cwd: "/home/me/gitdemo", command: "zsh", status: "running", started_at: 1 }];
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: sessions }, { "web.initialPath": { path: `/sessions/${id}`, state: null } }) });
    // Before the Session list arrives the tab says "Session"; then the page's title.
    await app.waitUntil(() => last(app)?.path === `/sessions/${id}` && last(app)?.title !== "Session · Mewla", { timeout: 10_000 });
    await app.wait(300);
    expect(last(app)!.title).toBe("gitdemo · Mewla");
  }, 20_000);

  test("a Session's URL is under Sessions and its tab says its name; another computer's entry never reopens its id", async () => {
    const sessions = [{ id: "w1", name: "api server", cwd: "/home/me/api", status: "running", started_at: 1 }];
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: sessions }, { "web.initialPath": { path: "/sessions/w1", state: null } }) });
    await app.waitUntil(() => last(app)?.path === "/sessions/w1" && last(app)?.title === "api server · Mewla");
    const entry = last(app)!;
    await app.press("id:sidebar-calendar");
    await app.waitUntil(() => last(app)?.path === "/calendar");
    await app.host.send("web.popstate", { path: entry.path, state: { ...entry.state, serverId: "another" } });
    await app.waitUntil(() => last(app)?.path === "/sessions");
  }, 20_000);

  test("the Expo web app's /terminal bookmark is not a place any more: it opens Brain", async () => {
    const sessions = [{ id: "w1", name: "api server", cwd: "/home/me/api", status: "running", started_at: 1 }];
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: sessions }, { "web.initialPath": { path: "/terminal/w1?serverId=old", state: null } }) });
    await app.waitUntil(() => last(app)?.path === "/");
    expect(locations(app)[0].mode).toBe("replace");
  }, 20_000);

  test("below 1024 pt the phone's one stack, no sidebar; the URL still follows", async () => {
    const app = await open(APP, { ...desk, width: 800, host: webHost() });
    await app.waitFor("id:home-header");
    expect(app.shows("id:sidebar")).toBe(false);
    await app.press("id:home-cat");
    await app.waitUntil(() => last(app)?.path === "/computer");
  }, 20_000);

  test("the sidebar's width is dragged and saved", async () => {
    const app = await open(APP, { ...desk, host: webHost() });
    await app.waitFor("id:sidebar");
    await app.drag("label:Resize sidebar", { x: 60 });
    await app.waitUntil(() => app.storage()["mewla.desktop.sidebarWidth"] !== undefined);
    expect(Number(app.storage()["mewla.desktop.sidebarWidth"])).toBeGreaterThan(264);
  }, 20_000);
});

describe("page headers on the web", () => {
  test("every page over home has Back, which goes back a history step", async () => {
    const sessions = [{ id: "w1", name: "api server", status: "done", cwd: "/home/me/api", command: "claude", provider: "claude", updated_at: Date.now() }];
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: sessions, conversations: { w1: [] } }) });
    await app.waitUntil(() => locations(app).length > 0);
    expect(app.count("id:page-back")).toBe(0);
    await app.press("id:sidebar-sessions");
    await app.waitUntil(() => last(app)?.path === "/sessions");
    await app.waitUntil(() => app.count("id:page-back") === 1);
    await app.waitFor("text:api server", { timeout: 10_000 });
    await app.press("text:api server");
    await app.waitUntil(() => last(app)?.path === "/sessions/w1");
    expect(app.shows("id:session-menu")).toBe(true);
    expect(app.count("id:page-back")).toBe(1);
    await app.press("id:page-back");
    // Back to the list is the browser's step back, so Forward still reopens the Session.
    await app.waitUntil(() => last(app)?.path === "/sessions" && last(app)?.mode === "back");
    await app.waitUntil(() => app.count("id:page-back") === 1);
  }, 20_000);
});

describe("Phone use's link", () => {
  test("mewla://phone opens the Phone use page", async () => {
    const status = { supported: true, sdk: 35, enabled: true, link: true, link_state: "online", developer_options: true, wireless_debugging: true, wifi: true, paired: true, helper: true, problem: null, accessibility: true, restricted: false, self_healing: true, guide: null, notifications: true, state: "ready", w: 720, h: 1600, display_info: null, agent: null, handoff: null, drawn: true, opening: null, launch_failed: null };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain: {} }, { "phone.status": () => status, "phone.follow": () => null, "phone.setPipAuto": () => null }) });
    await app.waitFor("id:home-header");
    await app.host.send("links.open", { url: "mewla://phone" });
    await app.waitFor("id:phone-page");
  }, 20_000);
});
