import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost, pressUntil, settle } from "../testing/fixtures";

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

type Routes = { name: string; workerId?: string }[];
/** The stack the app last gave the page host (its history entry's snapshot). */
const routes = (app: Parameters<typeof locations>[0]) => ((last(app)?.state.snapshot as { routes?: Routes } | undefined)?.routes ?? []).map((route) => route.name + (route.workerId ? `:${route.workerId}` : ""));

const running = (id: string, name: string) => ({ id, name, status: "running", cwd: `/home/me/${name}`, command: "claude", provider: "claude", started_at: 1, updated_at: Date.now() });

describe("the wide layout (SPEC §14)", () => {
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

  test("the left pane is the tray, always open: the computer and its state, Search or run, Brain, then the tray with its counts", async () => {
    const sessions = [running("w1", "api"), running("w2", "docs"), { ...running("w3", "release"), status: "blocked", needs_attention: true, summary: "Approve the deploy" }];
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: sessions }) });
    await app.waitFor("id:left-pane", { timeout: 10_000 });
    // The title is its counts, each a filter (many Workers 11-19); in the 320 pane "needs you" is its dot and number (core/tray/counts.ts).
    await app.waitUntil(() => app.shows("label:1 needs you") && app.texts().includes("2 running"), { timeout: 10_000 });
    expect(app.texts()).toContain("Desk");
    expect(app.texts()).toContain("Search or run");
    expect(app.texts()).toContain("Ctrl K");
    expect(app.shows("id:pane-brain")).toBe(true);
    expect(app.shows("id:tray-needs")).toBe(true);
    expect(app.count("id:tray-running >> id:tray-row")).toBe(2);
    expect(app.shows("id:tray-all-sessions")).toBe(true);
    // The pane holds the cat and the tray: Brain's header draws neither (no second cat, no state control).
    expect(app.count("id:home-cat")).toBe(1);
    expect(app.shows("id:state-control")).toBe(false);
    expect(app.shows("id:tray-overlay")).toBe(false);
    // Brain is the page shown: its row has the pressed well.
    expect(app.shows(`id:pane-brain >> id:selected-well`)).toBe(true);
  }, 20_000);

  test("a left-pane row replaces the page above Brain, so Back always leads to Brain", async () => {
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: [running("w1", "api"), running("w2", "docs")], conversations: { w1: [], w2: [] } }) });
    await app.waitFor("id:left-pane", { timeout: 10_000 });
    await app.waitUntil(() => app.count("id:tray-running >> id:tray-row") === 2, { timeout: 10_000 });
    await settle(app);
    await app.press("id:tray-running >> text:api");
    await app.waitUntil(() => routes(app).join(" ") === "home session:w1");
    expect(app.count("id:tray-running >> id:selected-well")).toBe(1);
    await app.press("id:tray-running >> text:docs");
    await app.waitUntil(() => routes(app).join(" ") === "home session:w2");
    await app.press("id:tray-all-sessions");
    await app.waitUntil(() => routes(app).join(" ") === "home allSessions");
    expect(app.shows(`id:tray-all-sessions >> id:selected-well`)).toBe(true);
    await app.press("id:home-cat");
    await app.waitUntil(() => routes(app).join(" ") === "home computer");
    await app.press("id:page-back");
    await app.waitUntil(() => routes(app).join(" ") === "home");
    expect(app.shows(`id:pane-brain >> id:selected-well`)).toBe(true);
  }, 20_000);

  test("a request row whose card is in Brain's thread returns the right pane to Brain; the pane stays", async () => {
    const prompt = { id: "p1", kind: "permission", provider: "claude", title: "Run this command?", request: "rm -rf build", options: [{ number: 1, label: "Yes" }, { number: 2, label: "No" }, { number: 3, label: "Tell Claude" }] };
    const brain = { host_worker: { id: "host", name: "brain", status: "blocked", prompt }, chat_thread_id: "t1" };
    const app = await open(APP, { ...desk, host: webHost({ brain, worker_sessions: [running("w1", "api")], conversations: { w1: [] } }) });
    await app.waitFor("id:tray-request", { timeout: 10_000 });
    await settle(app);
    await app.press("id:tray-running >> text:api");
    await app.waitUntil(() => routes(app).join(" ") === "home session:w1");
    await app.press("id:tray-needs >> text:Brain");
    await app.waitUntil(() => routes(app).join(" ") === "home");
    expect(app.shows("id:left-pane")).toBe(true);
    expect(app.shows("id:pane-brain >> id:selected-well")).toBe(true);
  }, 20_000);

  test("a right-click on a Session row starts selecting, as a long press does on touch (parity #28)", async () => {
    const sessions = [{ id: "w1", name: "release", status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() }];
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: sessions }) });
    await app.waitFor("id:tray-all-sessions", { timeout: 10_000 });
    await app.press("id:tray-all-sessions");
    await app.waitFor("id:session-row");
    await app.rightClick("id:session-row");
    await app.waitFor("text:1 selected");
  }, 20_000);

  test("a shared URL to a sub-page opens that page, and the page keeps its URL (parity #47)", async () => {
    const app = await open(APP, { ...desk, clock: {}, host: webHost({ brain: {} }, { "web.initialPath": { path: "/settings/computer?id=s1", state: null } }) });
    await app.waitFor("id:computer", { timeout: 10_000 });
    await app.waitUntil(() => last(app)?.path === "/settings/computer?id=s1", { timeout: 10_000 });
    await app.keyboard.press("g");
    await app.keyboard.press("t");
    await app.waitUntil(() => last(app)?.path === "/stats");
  }, 20_000);

  test("every place has a URL and the tab says \"{page} · Mewla\"", async () => {
    const app = await open(APP, { ...desk, clock: {}, host: webHost() });
    await app.waitFor("id:left-pane");
    await app.waitUntil(() => locations(app).length > 0);
    expect(locations(app)[0]).toMatchObject({ path: "/", title: "Brain · Mewla", mode: "replace", state: { index: 0, serverId: "s1" } });
    const first = (path: string) => locations(app).find((location) => location.path === path);
    await app.keyboard.press("g");
    await app.keyboard.press("c");
    await app.waitUntil(() => last(app)?.path === "/calendar");
    expect(first("/calendar")).toMatchObject({ title: "Calendar · Mewla", mode: "push", state: { index: 1 } });
    await app.press("id:tray-all-sessions");
    await app.waitUntil(() => last(app)?.path === "/sessions");
    expect(first("/sessions")).toMatchObject({ title: "All Sessions · Mewla", mode: "push", state: { index: 2 } });
    await app.keyboard.press("g");
    await app.keyboard.press(",");
    await app.waitFor("id:settings");
    await app.waitUntil(() => last(app)?.path === "/settings");
    expect(first("/settings")).toMatchObject({ title: "Settings · Mewla", mode: "push" });
    // Nothing changed, nothing sent again: one write per place (browsers cap history writes).
    await app.wait(300);
    expect(locations(app).filter((location) => location.path === "/calendar")).toHaveLength(1);
    expect(locations(app).filter((location) => location.path === "/settings")).toHaveLength(1);
  }, 20_000);

  test("Back and Forward reopen what an entry showed; an entry without the app's state goes by its path", async () => {
    const app = await open(APP, { ...desk, clock: {}, host: webHost() });
    await app.waitUntil(() => locations(app).length > 0);
    await app.keyboard.press("g");
    await app.keyboard.press("t");
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

  test("a Needs you row of a Session is selected while its Session is the page, as a running row is", async () => {
    const ask = { id: "p1", kind: "permission", provider: "claude", title: "Run this command?", request: "git push", options: [{ number: 1, label: "Yes" }, { number: 2, label: "No" }] };
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: [{ ...running("w1", "api"), status: "blocked", prompt: ask }], conversations: { w1: [] } }) });
    await app.waitFor("id:tray-request", { timeout: 10_000 });
    expect(app.count("id:tray-request >> id:selected-well")).toBe(0);
    await app.press("id:tray-request");
    await app.waitUntil(() => routes(app).includes("session:w1"));
    await app.waitFor("id:tray-request >> id:selected-well");
  }, 20_000);

  test("a Work's detail has a URL: opened at it, the page is that Work over Brain, and the app's own link path reads the same", async () => {
    const brain = { current_work: [{ work_id: "docsref", revision: 1, title: "docs-refresh", status: "running", progress_mode: "owned", unread_result: false, updated_at: new Date().toISOString() }] };
    for (const path of ["/work-detail/docsref", "/brain/work/docsref"]) {
      const app = await open(APP, { ...desk, host: webHost({ brain }, { "web.initialPath": { path, state: null } }) });
      await app.waitFor("id:work-detail", { timeout: 10_000 });
      await app.waitUntil(() => last(app)?.path === "/work-detail/docsref");
      expect(routes(app)).toEqual(["home", "workDetail"]);
    }
  }, 30_000);

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
    await app.press("id:tray-all-sessions");
    await app.waitUntil(() => last(app)?.path === "/sessions");
    await app.host.send("web.popstate", { path: entry.path, state: { ...entry.state, serverId: "another" } });
    await app.waitUntil(() => last(app)?.path === "/sessions");
  }, 20_000);

  test("below 1024 pt no left pane: Brain's header has the cat and the state control; the URL still follows", async () => {
    const app = await open(APP, { ...desk, width: 800, host: webHost() });
    await app.waitFor("id:home-header");
    expect(app.shows("id:left-pane")).toBe(false);
    expect(app.shows("id:state-control")).toBe(true);
    await settle(app);
    await app.press("id:home-cat");
    await app.waitUntil(() => last(app)?.path === "/computer");
  }, 20_000);

  test("the handle: drag within 280–420 (saved), ← and → step 16 when focused, double-click resets to 320", async () => {
    const app = await open(APP, { ...desk, clock: {}, host: webHost() });
    await app.waitFor("id:pane-handle");
    await settle(app);
    await app.drag("id:pane-handle", { x: 60 });
    await app.waitUntil(() => app.storage()["mewla.wide.paneWidth"] === "380");
    await app.drag("id:pane-handle", { x: 300 });
    await app.waitUntil(() => app.storage()["mewla.wide.paneWidth"] === "420");
    // The drag gave it the focus: the arrows are its.
    await app.keyboard.press("left");
    await app.waitUntil(() => app.storage()["mewla.wide.paneWidth"] === "404");
    await app.keyboard.press("right");
    await app.waitUntil(() => app.storage()["mewla.wide.paneWidth"] === "420");
    // Two presses with the clock held: a double click.
    await app.press("id:pane-handle");
    await app.press("id:pane-handle");
    await app.waitUntil(() => app.storage()["mewla.wide.paneWidth"] === "320");
  }, 20_000);

});

describe("list and detail follow the right pane (SPEC §14.2, §14.6)", () => {
  test("Browse workspace at 1280 shows its file beside the folder; the handle narrowing the right pane below 880 makes it push, and back", async () => {
    const tree = { type: "brain_workspace_tree", workspace_tree: { workspace: "/home/me/brain", path: "", entries: [{ name: "current.md", path: "current.md", kind: "file" }] } };
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, replies: { brain_workspace_tree: tree } }) });
    await app.waitFor("id:left-pane");
    await settle(app);
    await app.keyboard.press("mod+k");
    await app.waitFor("id:palette");
    await app.type("label:Search commands", "browse");
    await app.waitFor("id:palette >> text:Browse workspace");
    await app.keyboard.press("enter");
    // 1280 − 320 − the handle: a 952 pt right pane.
    await app.waitFor("id:workspace-reader");
    await app.drag("id:pane-handle", { x: 100 });
    await app.waitFor("id:workspace-reader", { state: "gone" });
    expect(app.shows("id:workspace-current.md")).toBe(true);
    await app.drag("id:pane-handle", { x: -100 });
    await app.waitFor("id:workspace-reader");
  }, 20_000);
});

describe("crossing 1024 (SPEC §14.6)", () => {
  for (const platform of ["web", "android", "ios"] as const) {
    test(`${platform}: the same stack, the same page and what is typed, both ways, with nothing made again`, async () => {
      const sessions = [{ id: "w1", name: "release", status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() }];
      const host = platform === "web" ? webHost({ brain: {}, worker_sessions: sessions }) : phoneHost({ brain: {}, worker_sessions: sessions });
      const app = await open(APP, { platform, width: 1280, height: 800, storage: paired, host });
      await app.waitFor("id:left-pane", { timeout: 10_000 });
      await settle(app);
      // What is typed in Brain's field stays in the field, both ways.
      await app.type("id:composer", "half a thought");
      await app.waitUntil(() => app.value("id:composer") === "half a thought");
      await app.setViewport(800, 1280);
      await app.waitFor("id:left-pane", { state: "gone" });
      expect(app.value("id:composer")).toBe("half a thought");
      expect(app.shows("id:state-control")).toBe(true);
      await app.setViewport(1280, 800);
      await app.waitFor("id:left-pane");
      expect(app.value("id:composer")).toBe("half a thought");
      expect(app.shows("id:state-control")).toBe(false);
      // A page and what only that mounted page holds (All Sessions' selection) stay too.
      await app.press("id:tray-all-sessions");
      await app.waitFor("id:session-row");
      await app.longPress("id:session-row");
      await app.waitFor("text:1 selected");
      await app.setViewport(800, 1280);
      await app.waitFor("id:left-pane", { state: "gone" });
      expect(app.shows("text:1 selected")).toBe(true);
      await app.setViewport(1280, 800);
      await app.waitFor("id:left-pane");
      expect(app.shows("text:1 selected")).toBe(true);
      // Narrowed again, Back from it still leads to Brain, with its cat and state control back in the header.
      await app.setViewport(800, 1280);
      await app.waitFor("id:left-pane", { state: "gone" });
      for (let i = 0; i < 3 && !app.shows("id:home-header"); i += 1) await app.back();
      await app.waitFor("id:home-header");
      expect(app.shows("id:state-control")).toBe(true);
    }, 30_000);
  }

  test("narrow to wide: an open tray becomes the left pane, and does not come back over Brain when narrowed again", async () => {
    const app = await open(APP, { platform: "android", width: 800, height: 1280, storage: paired, host: phoneHost({ brain: {}, worker_sessions: [running("w1", "api")] }) });
    await app.waitFor("id:state-control", { timeout: 10_000 });
    await settle(app);
    await pressUntil(app, "id:state-control", "id:tray-overlay");
    await app.setViewport(1280, 800);
    await app.waitFor("id:left-pane");
    expect(app.shows("id:tray-overlay")).toBe(false);
    await app.setViewport(800, 1280);
    await app.waitFor("id:left-pane", { state: "gone" });
    expect(app.shows("id:tray-overlay")).toBe(false);
  }, 30_000);
});

describe("dialogs and popovers on wide (SPEC §14.2)", () => {
  test("a sheet is a centred dialog without a grabber; Esc closes it", async () => {
    const app = await open(APP, { ...desk, host: webHost() });
    await app.waitFor("id:left-pane");
    await settle(app);
    await app.press("id:tray-new");
    await app.waitFor("id:new-session-sheet");
    await app.keyboard.press("escape");
    await app.waitFor("id:new-session-sheet", { state: "gone" });
  }, 20_000);

  test("Brain's ⋯ is a popover at its button; Esc and a press outside close it. Below 1024 it is the sheet", async () => {
    const app = await open(APP, { ...desk, host: webHost({ brain: { host_worker: { id: "host", name: "brain", status: "running" }, chat_thread_id: "t1" } }) });
    await app.waitFor("id:left-pane");
    await settle(app);
    await app.press("id:brain-menu");
    await app.waitFor("id:brain-menu-popover");
    expect(app.shows("id:menu-item-terminal")).toBe(true);
    expect(app.shows("id:brain-menu-sheet")).toBe(false);
    await app.keyboard.press("escape");
    await app.waitFor("id:brain-menu-popover", { state: "gone" });
    // A press outside it closes it too.
    await app.press("id:brain-menu");
    await app.waitFor("id:brain-menu-popover");
    await app.press("id:pane-brain");
    await app.waitFor("id:brain-menu-popover", { state: "gone" });
    await app.setViewport(800, 1280);
    await app.press("id:brain-menu");
    await app.waitFor("id:brain-menu-sheet");
    expect(app.shows("id:brain-menu-popover")).toBe(false);
  }, 20_000);

  test("the tray's ⋯ is a popover; Sort is a second popover with the order checked", async () => {
    const app = await open(APP, { ...desk, host: webHost() });
    await app.waitFor("id:left-pane");
    await settle(app);
    await app.press("id:tray-menu");
    await app.waitFor("id:tray-menu-popover");
    await app.press("id:menu-item-sort");
    await app.waitFor("id:menu-item-activity");
    await app.press("id:menu-item-activity");
    await app.waitUntil(() => app.storage()["mewla:tray:sort"] === "activity");
    expect(app.shows("id:menu-popover")).toBe(false);
  }, 20_000);
});

describe("page headers on the web", () => {
  test("every page over home has Back, which goes back a history step", async () => {
    const sessions = [{ id: "w1", name: "api server", status: "done", cwd: "/home/me/api", command: "claude", provider: "claude", updated_at: Date.now() }];
    const app = await open(APP, { ...desk, host: webHost({ brain: {}, worker_sessions: sessions, conversations: { w1: [] } }) });
    await app.waitUntil(() => locations(app).length > 0);
    expect(app.count("id:page-back")).toBe(0);
    await app.press("id:tray-all-sessions");
    await app.waitUntil(() => last(app)?.path === "/sessions");
    await app.waitUntil(() => app.count("id:page-back") === 1);
    await app.waitFor("id:session-row", { timeout: 10_000 });
    await app.press("id:session-row");
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

describe("beside: a wide window opens what you pick next to Brain (core/web/beside.ts)", () => {
  const big = { platform: "web" as const, width: 1920, height: 1080, storage: paired };
  const work = [
    { work_id: "docsref", revision: 1, title: "docs-refresh", status: "running", progress_mode: "owned", unread_result: false, updated_at: new Date().toISOString() },
    { work_id: "naming", revision: 1, title: "pick-a-name", status: "needs_input", progress_mode: "waiting", unread_result: false, question: "Which name?", attention_since: new Date().toISOString() },
  ];

  test("from 1920 the beside pane stands next to Brain with the Work board; 1440 keeps one page", async () => {
    const app = await open(APP, { ...big, host: webHost({ brain: { current_work: work } }) });
    await app.waitFor("id:beside-pane", { timeout: 10_000 });
    await app.waitFor("id:work-board-needs");
    expect(app.count("id:work-board-card")).toBe(2);
    expect(app.texts()).toContain("Which name?");
    expect(app.shows("id:home-header")).toBe(true);
    await app.setViewport(1440, 900);
    await app.waitUntil(() => !app.shows("id:beside-pane"));
    expect(app.shows("id:home-header")).toBe(true);
  }, 20_000);

  test("on an ultrawide the board is columns side by side, and from 2560 Brain's column and its type are a step larger", async () => {
    const app = await open(APP, { ...big, width: 3440, height: 1440, host: webHost({ brain: { current_work: work }, worker_sessions: [running("w1", "api"), running("w2", "docs")], conversations: { w1: [], w2: [] } }) });
    await app.waitFor("id:beside-pane", { timeout: 10_000 });
    await app.waitFor("id:work-board-running");
    await app.waitUntil(() => app.count("id:work-board-column") >= 2);
    const columns = Array.from({ length: app.count("id:work-board-column") }, (_, i) => app.layout(`id:work-board-column >> nth=${i}`)!);
    // Side by side, one top line, filling the pane: not one narrow column.
    expect(new Set(columns.map((c) => Math.round(c.y))).size).toBe(1);
    const pane = app.layout("id:beside-pane")!;
    const used = columns.reduce((sum, c) => sum + c.width, 0);
    expect(used / pane.width).toBeGreaterThan(0.85);
    expect(Math.round(pane.x - app.layout("id:right-pane")!.x)).toBe(880);
    await app.setViewport(1920, 1080);
    await app.waitUntil(() => Math.round(app.layout("id:beside-pane")!.x - app.layout("id:right-pane")!.x) === 800);
  }, 20_000);

  test("a row and a card open beside Brain; Back returns the pane to the board, and the URL names what is open", async () => {
    const app = await open(APP, { ...big, host: webHost({ brain: { current_work: work }, worker_sessions: [running("w1", "api"), running("w2", "docs")], conversations: { w1: [], w2: [] } }) });
    await app.waitFor("id:beside-pane", { timeout: 10_000 });
    await app.waitUntil(() => app.count("id:tray-running >> id:tray-row") >= 2, { timeout: 10_000 });
    await settle(app);
    await app.press("id:tray-running >> text:api");
    await app.waitUntil(() => routes(app).join(" ") === "home session:w1");
    await app.waitFor("id:beside-pane >> id:session-status");
    expect(app.layout("id:session-status")!.width).toBeGreaterThan(200);
    // Brain stays: its composer and the pane's Session side by side, the row selected.
    expect(app.shows("id:home-header")).toBe(true);
    expect(app.count("id:tray-running >> id:selected-well")).toBe(1);
    // Another row takes its place: never a deeper stack.
    await app.press("id:tray-running >> text:docs");
    await app.waitUntil(() => routes(app).join(" ") === "home session:w2");
    // A Work card on the board opens its detail beside Brain.
    await app.press("id:beside-pane >> id:page-back");
    await app.waitFor("id:work-board");
    await app.waitUntil(() => routes(app).join(" ") === "home");
    await app.press("id:work-board-card >> text:Which name?");
    await app.waitFor("id:beside-pane >> id:work-detail");
    await app.waitUntil(() => last(app)?.path === "/work-detail/naming");
  }, 30_000);

  test("narrowed, what stood beside Brain goes on the stack over it; widened, it stands beside again", async () => {
    const app = await open(APP, { ...big, host: webHost({ brain: {}, worker_sessions: [running("w1", "api")], conversations: { w1: [] } }) });
    await app.waitFor("id:beside-pane", { timeout: 10_000 });
    await app.waitUntil(() => app.count("id:tray-running >> id:tray-row") === 1, { timeout: 10_000 });
    await settle(app);
    await app.press("id:tray-running >> text:api");
    await app.waitUntil(() => routes(app).join(" ") === "home session:w1");
    await app.setViewport(1280, 800);
    await app.waitUntil(() => !app.shows("id:beside-pane"));
    await app.waitFor("id:session-status");
    expect(app.shows("id:home-header")).toBe(false);
    expect(routes(app).join(" ")).toBe("home session:w1");
    await app.setViewport(1920, 1080);
    await app.waitFor("id:beside-pane >> id:session-status");
    expect(app.shows("id:home-header")).toBe(true);
    expect(routes(app).join(" ")).toBe("home session:w1");
  }, 30_000);

  test("a page that is not a Session or a Work (Settings) covers both; Back finds the pane as it was", async () => {
    const app = await open(APP, { ...big, host: webHost({ brain: {}, worker_sessions: [running("w1", "api")], conversations: { w1: [] } }) });
    await app.waitFor("id:beside-pane", { timeout: 10_000 });
    await app.waitUntil(() => app.count("id:tray-running >> id:tray-row") === 1, { timeout: 10_000 });
    await settle(app);
    await app.press("id:tray-running >> text:api");
    await app.waitUntil(() => routes(app).join(" ") === "home session:w1");
    await app.press("id:home-cat");
    await app.waitUntil(() => routes(app).join(" ") === "home computer");
    expect(app.shows("id:home-header")).toBe(false);
    // The pane is kept under the page (display none), so its own Back is still in the tree, laid out clipped to nothing.
    expect(app.shows("id:beside-pane")).toBe(false);
    await app.press("id:page-back >> nth=0");
    await app.waitUntil(() => routes(app).join(" ") === "home session:w1");
    expect(app.shows("id:beside-pane >> id:session-status")).toBe(true);
  }, 30_000);

  test("on an ultrawide a Session shows its chat and its terminal side by side, with no view switch", async () => {
    const app = await open(APP, { platform: "web", width: 3440, height: 1440, storage: paired, host: webHost({ brain: {}, worker_sessions: [running("w1", "api")], conversations: { w1: [] } }) });
    await app.waitFor("id:beside-pane", { timeout: 10_000 });
    await app.waitUntil(() => app.count("id:tray-running >> id:tray-row") === 1, { timeout: 10_000 });
    await settle(app);
    await app.press("id:tray-running >> text:api");
    await app.waitFor("id:beside-pane >> id:session-chat");
    await app.waitFor("id:beside-pane >> id:terminal-group");
    expect(app.shows("id:session-mode")).toBe(false);
    await app.setViewport(2560, 1440);
    await app.waitFor("id:session-mode");
    expect(app.shows("id:terminal-group")).toBe(false);
  }, 30_000);

  test("a tablet as wide as an ultrawide keeps its one stack: beside is the desktops' and the web's", async () => {
    const app = await open(APP, { platform: "android", width: 2560, height: 1600, storage: paired, host: phoneHost({ brain: {} }) });
    await app.waitFor("id:left-pane", { timeout: 10_000 });
    await app.wait(300);
    expect(app.shows("id:beside-pane")).toBe(false);
  }, 20_000);

  for (const [platform, width, height] of [["android", 390, 844], ["ios", 390, 844], ["android", 1440, 900]] as const) {
    test(`${platform} ${width}: a file Brain names covers the thread on the native stack, as before (no preview split)`, async () => {
      const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
      const reply = { id: "a1", seq: 1, timestamp: new Date().toISOString(), kind: "assistant_message", role: "assistant", body: "I wrote [the plan](/home/me/worklog/plan.md)." };
      const app = await open(APP, { platform, width, height, storage: paired, host: phoneHost({ brain: { chat_thread_id: "t-now", host_worker }, conversations: { "brain-host": [reply] } }) });
      await app.waitFor("text:the plan", { timeout: 10_000 });
      await app.press("text:the plan");
      await app.waitFor("id:file-preview", { timeout: 10_000 });
      expect(app.shows("id:beside-pane")).toBe(false);
      expect(app.shows("id:split-handle")).toBe(false);
      expect(app.shows("id:page-close")).toBe(false);
    }, 20_000);
  }

  test("Back from what stood beside Brain returns the pane to the board; the Brain row and G then B do too", async () => {
    const app = await open(APP, { ...big, clock: {}, host: webHost({ brain: {}, worker_sessions: [running("w1", "api")], conversations: { w1: [] } }) });
    await app.waitFor("id:beside-pane", { timeout: 10_000 });
    await app.waitUntil(() => app.count("id:tray-running >> id:tray-row") === 1, { timeout: 10_000 });
    await settle(app);
    const brain = locations(app)[0];
    await app.press("id:tray-running >> text:api");
    await app.waitUntil(() => last(app)?.path === "/sessions/w1");
    // The browser's Back to Brain's entry.
    await app.host.send("web.popstate", { path: brain.path, state: brain.state });
    await app.waitFor("id:work-board");
    await app.waitUntil(() => last(app)?.path === "/");
    expect(app.shows("id:session-status")).toBe(false);
    // The Brain row.
    await app.press("id:tray-running >> text:api");
    await app.waitUntil(() => last(app)?.path === "/sessions/w1");
    await app.press("id:pane-brain");
    await app.waitFor("id:work-board");
    await app.waitUntil(() => last(app)?.path === "/");
    expect(app.shows("id:pane-brain >> id:selected-well")).toBe(true);
    // G then B.
    await app.press("id:tray-running >> text:api");
    await app.waitUntil(() => last(app)?.path === "/sessions/w1");
    await app.keyboard.press("g");
    await app.keyboard.press("b");
    await app.waitFor("id:work-board");
    await app.waitUntil(() => last(app)?.path === "/");
  }, 30_000);

  test("a file Brain names opens its preview beside the thread", async () => {
    const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
    const reply = { id: "a1", seq: 1, timestamp: new Date().toISOString(), kind: "assistant_message", role: "assistant", body: "I wrote [the plan](/home/me/worklog/plan.md)." };
    const app = await open(APP, { ...big, host: webHost({ brain: { chat_thread_id: "t-now", host_worker }, conversations: { "brain-host": [reply] } }) });
    await app.waitFor("text:the plan", { timeout: 10_000 });
    await app.press("text:the plan");
    await app.waitFor("id:beside-pane >> id:file-preview", { timeout: 10_000 });
    expect(app.shows("id:home-header")).toBe(true);
    await app.press("id:beside-pane >> id:page-back");
    await app.waitFor("id:work-board");
  }, 20_000);

  test("on a laptop (1440) a file Brain names opens beside the thread, which keeps its composer; × gives the thread its width back", async () => {
    const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
    const reply = { id: "a1", seq: 1, timestamp: new Date().toISOString(), kind: "assistant_message", role: "assistant", body: "I wrote [the plan](/home/me/worklog/plan.md) and [the notes](/home/me/worklog/notes.md)." };
    const app = await open(APP, { ...big, width: 1440, height: 900, host: webHost({ brain: { chat_thread_id: "t-now", host_worker, current_work: work }, conversations: { "brain-host": [reply] } }) });
    await app.waitFor("text:the plan", { timeout: 10_000 });
    // Nothing open: the thread has the right pane, no board.
    expect(app.shows("id:beside-pane")).toBe(false);
    expect(app.shows("id:work-board-card")).toBe(false);
    await app.press("text:the plan");
    await app.waitFor("id:beside-pane >> id:file-preview", { timeout: 10_000 });
    expect(app.shows("id:home-header")).toBe(true);
    expect(app.shows("id:split-handle")).toBe(true);
    // The first preview has no Back: × closes the pane.
    expect(app.shows("id:beside-pane >> id:page-back")).toBe(false);
    expect(app.shows("id:beside-pane >> id:page-close")).toBe(true);
    // The person keeps typing to Brain beside it.
    await app.type("id:composer", "beside the plan");
    expect(app.shows("id:beside-pane >> id:file-preview")).toBe(true);
    // A second file goes over the first: its Back returns to the plan.
    await app.press("text:the notes");
    await app.waitUntil(() => app.shows("id:beside-pane >> id:page-back"));
    await app.press("id:beside-pane >> id:page-back");
    await app.waitUntil(() => !app.shows("id:beside-pane >> id:page-back"));
    expect(app.shows("id:beside-pane >> id:file-preview")).toBe(true);
    await app.press("id:beside-pane >> id:page-close");
    await app.waitUntil(() => !app.shows("id:beside-pane"));
    expect(app.shows("id:home-header")).toBe(true);
    expect(app.shows("id:work-board-card")).toBe(false);
  }, 30_000);

  test("on a laptop a link inside a preview opens the file in the pane, over the first, and Back returns to it", async () => {
    const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
    const reply = { id: "a1", seq: 1, timestamp: new Date().toISOString(), kind: "assistant_message", role: "assistant", body: "I wrote [the plan](/home/me/worklog/plan.md)." };
    const metadata = { name: "plan.md", path: "/home/me/worklog/plan.md", relative_path: "worklog/plan.md", kind: "markdown", content_type: "text/markdown", size: 40, modified_at: "2026-11-11T08:00:00Z", generation: "g1" };
    const replies = {
      session_file_metadata: { type: "session_file_metadata", metadata },
      session_file_text: { type: "session_file_text", text: { content: "# Plan\n\nSee [the steps](steps.md).", bytes_read: 40, truncated: false, generation: "g1" } },
    };
    const app = await open(APP, { ...big, width: 1440, height: 900, host: webHost({ brain: { chat_thread_id: "t-now", host_worker }, conversations: { "brain-host": [reply] }, replies }) });
    await app.waitFor("text:the plan", { timeout: 10_000 });
    await app.press("text:the plan");
    await app.waitFor("id:beside-pane >> text:the steps", { timeout: 10_000 });
    expect(app.shows("id:beside-pane >> id:page-back")).toBe(false);
    await app.press("id:beside-pane >> text:the steps");
    await app.waitUntil(() => app.shows("id:beside-pane >> id:page-back"));
    // Still beside the thread, not over both.
    expect(app.shows("id:home-header")).toBe(true);
    await app.press("id:beside-pane >> id:page-back");
    await app.waitUntil(() => !app.shows("id:beside-pane >> id:page-back"));
    expect(app.shows("id:beside-pane >> id:file-preview")).toBe(true);
  }, 30_000);

  test("on a laptop a preview covers the thread below 1028 of right pane and stands beside it again when widened", async () => {
    const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
    const reply = { id: "a1", seq: 1, timestamp: new Date().toISOString(), kind: "assistant_message", role: "assistant", body: "I wrote [the plan](/home/me/worklog/plan.md)." };
    const app = await open(APP, { ...big, width: 1440, height: 900, host: webHost({ brain: { chat_thread_id: "t-now", host_worker }, conversations: { "brain-host": [reply] } }) });
    await app.waitFor("text:the plan", { timeout: 10_000 });
    await app.press("text:the plan");
    await app.waitFor("id:beside-pane >> id:file-preview", { timeout: 10_000 });
    // 1280 less the 320 pane is 960: the preview covers Brain, still open, with a Back to the thread.
    await app.setViewport(1280, 800);
    await app.waitUntil(() => !app.shows("id:beside-pane"));
    expect(app.shows("id:file-preview")).toBe(true);
    expect(app.shows("id:page-back")).toBe(true);
    expect(app.shows("id:home-header")).toBe(false);
    await app.setViewport(1440, 900);
    await app.waitFor("id:beside-pane >> id:file-preview");
    expect(app.shows("id:home-header")).toBe(true);
    // Widened to the board (1920): the preview stays beside, the thread at its 800.
    await app.setViewport(1920, 1080);
    await app.waitUntil(() => Math.round(app.layout("id:beside-pane")!.x - app.layout("id:right-pane")!.x) === 800);
    expect(app.shows("id:beside-pane >> id:file-preview")).toBe(true);
    expect(app.shows("id:beside-pane >> id:page-back")).toBe(true);
  }, 30_000);
});
