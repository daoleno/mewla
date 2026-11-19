import { describe, expect, open, test, type StubResponse } from "nsh/test";
import { expoTokenService, paired, phoneHost, stepUntil } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const BASE = "http://127.0.0.1:1";

const json = (body: unknown): StubResponse => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
const session = (id: string, patch: Record<string, unknown> = {}) => ({ id, name: "", status: "running", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now(), ...patch });
const display = (id: string, patch: Record<string, unknown> = {}) => ({ id, width: 1280, height: 800, control: "agent", apps: 1, idle_s: 2, ...patch });
const linux = { supported: false, platform: "linux", name: "Desk", ready: false, video: "webrtc" };
/** Brain's thread with one Work, whose card holds its one Worker's row. */
const workThread = (id: string, name: string) => ({
  brain: { chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-15T07:00:00.000Z" },
    current_work: [{ work_id: "w1", revision: 1, title: "Sign in", status: "running", unread_result: false, attempt_session_id: id,
      worker_sessions: [{ session_id: id, name, provider: "claude", state: "done", summary: "The page is open for you", live: true, joined_at: "2026-11-15T14:43:00Z", updated_at: "2026-11-15T14:52:00Z" }] }] },
  conversations: { "brain-host": [{ id: "wr1", seq: 1, timestamp: "2026-11-15T14:52:00.000Z", kind: "status", source: "work_result", status: "session.done", title: "Sign in", body: "The page is open for you.",
    work_id: "w1", work_session_id: id, session_name: name, unread: false, work_review_state: "resolved", work_session_state: "finalized", work_result_current: true }] },
  worker_sessions: [session(id, { name, status: "done", delegated: true })],
});

/** The whole app, its computer answering Screen's and the displays' requests. */
async function app(daemon: Record<string, unknown>, network: Record<string, StubResponse>, platform: "android" | "ios" = "android", extra: Parameters<typeof open>[1] = {}) {
  const opened = await open(APP, { host: phoneHost({ brain: {}, ...daemon }), storage: paired, platform, network: { ...expoTokenService, ...network }, ...extra });
  await opened.waitFor("id:composer");
  return opened;
}

describe("the floating screen: agents' displays and the Mac", () => {
  test("on a wide window it docks in a strip beside the page, sized by the strip (no resize handle); at 1024 it floats in its corner", async () => {
    const network = {
      [`GET ${BASE}/display`]: json({ displays: [display("%1")], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(linux),
    };
    const daemon = { worker_sessions: [session("%1", { name: "Writer" })] };
    const docked = await app(daemon, network, "android", { width: 1440, height: 900 });
    await docked.waitFor("id:screen-float", { timeout: 10_000 });
    expect(docked.shows("id:left-pane")).toBe(true);
    expect(docked.shows("id:screen-float-resize")).toBe(false);
    const cornered = await app(daemon, network, "android", { width: 1024, height: 768 });
    await cornered.waitFor("id:screen-float", { timeout: 10_000 });
    expect(cornered.shows("id:screen-float-resize")).toBe(true);
  }, 25_000);

  test("an agent's display floats while its Worker runs, named after it; Stop in its menu stops that display", async () => {
    const shown = await app({ worker_sessions: [session("%1", { name: "Writer" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%1")], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(linux),
      [`POST ${BASE}/display`]: json({ ok: true }),
    });
    let offered: string[] = [];
    shown.onDialog((dialog) => {
      if (!dialog.sheet) return null;
      offered = dialog.buttons;
      return "Stop";
    });
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    expect(shown.texts()).toContain("Writer");
    // The float is for watching: no keyboard of its own.
    expect(shown.shows("id:screen-float-keyboard")).toBe(false);
    await shown.press("id:screen-float-options");
    await stepUntil(shown, () => offered.length > 0);
    expect(offered).toContain("Take over");
    expect(offered).toContain("Stop");
    await shown.waitUntil(() => shown.requests().some((r) => r.method === "POST" && r.url === `${BASE}/display`));
    const halt = shown.requests().find((r) => r.method === "POST" && r.url === `${BASE}/display`)!;
    expect(JSON.parse(String(halt.body))).toMatchObject({ worker: "%1" });
  }, 25_000);

  test("Open goes to Screen, where the float is not; a display its agent has stopped, or one long idle, does not float", async () => {
    const shown = await app({ worker_sessions: [session("%1", { name: "Writer" }), session("%2", { name: "Coder" }), session("%3", { name: "Idle" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%1"), display("%2", { stopped: true }), display("%3", { idle_s: 600 })], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(linux),
    });
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    expect(shown.texts()).not.toContain("Coder");
    expect(shown.texts()).not.toContain("Idle");
    expect(shown.texts().some((text) => text.includes("1/"))).toBe(false);
    await shown.press("id:screen-float-open");
    await shown.waitFor("id:screen-page", { timeout: 10_000 });
    await shown.waitUntil(() => !shown.shows("id:screen-float"));
  }, 25_000);

  test("nothing floats while no Worker runs: the displays are read once (a page left for the person), the Mac's screen not at all", async () => {
    const shown = await app({ worker_sessions: [session("%1", { name: "Writer", status: "done" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%1")], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(linux),
    });
    await shown.wait(1_500);
    expect(shown.shows("id:screen-float")).toBe(false);
    expect(shown.requests().filter((r) => r.url === `${BASE}/display`).length).toBe(1);
    expect(shown.requests().some((r) => r.url === `${BASE}/screen?video=webrtc`)).toBe(false);
  }, 25_000);

  test("a Worker whose turn ended with its sign-in page open: its Work card's Screen opens Screen at its display, named after it", async () => {
    const id = "mh-signin";
    const shown = await app(workThread(id, "ZeroGAccounts"), {
      [`GET ${BASE}/display`]: json({ displays: [display(id, { apps: 0, browser: 1, idle_s: 600 })], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json({ ...linux, agent_displays: true }),
    });
    await shown.waitFor("id:work-card-screen", { timeout: 10_000 });
    expect(shown.shows("id:screen-float")).toBe(false);
    // Its turn is over and its page is open for the person: the card takes it over in one step.
    expect(shown.texts()).toContain("Take over");
    await shown.press("id:work-card-screen");
    await shown.waitFor(`id:display-${id}`, { timeout: 10_000 });
    expect(shown.texts()).toContain("ZeroGAccounts");
  }, 25_000);

  test("on a Mac, a Chrome-only Worker's sign-in window: its Work card's Screen opens Screen at that window, not the Mac's own screen", async () => {
    const id = "mh-signin";
    const mac = { supported: true, platform: "darwin", name: "Studio", disabled: false, approved: true, ready: true, keep_awake: true, permissions: [], video: "webrtc" };
    const shown = await app(workThread(id, "SignIn"), {
      [`GET ${BASE}/display`]: json({ displays: [display(id, { name: "SignIn", width: 800, height: 500, apps: 0, browser: 1, idle_s: 600 })], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(mac),
    });
    await shown.waitFor("id:work-card-screen", { timeout: 10_000 });
    await shown.press("id:work-card-screen");
    await shown.waitFor(`id:display-${id}`, { timeout: 10_000 });
    expect(shown.texts()).toContain("SignIn");
    // The Mac's own screen is one step away, in the stage's menu.
    await shown.press("id:screen-options");
    await shown.waitFor("id:displays-own-screen");
  }, 25_000);

  test("on a Mac, a running Chrome-only Worker's window floats under its name", async () => {
    const mac = { supported: true, platform: "darwin", name: "Studio", disabled: false, approved: true, ready: true, keep_awake: true, permissions: [], video: "webrtc" };
    const shown = await app({ worker_sessions: [session("mh-signin", { name: "SignIn" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("mh-signin", { width: 800, height: 500, apps: 0, browser: 1, idle_s: 1 })], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(mac),
    });
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    expect(shown.texts()).toContain("SignIn");
  }, 25_000);

  test("a display with nothing left on it gives the Work card no Screen", async () => {
    const id = "mh-empty";
    const shown = await app(workThread(id, "Tidy"), {
      [`GET ${BASE}/display`]: json({ displays: [display(id, { apps: 0 })], available: true, video: "webrtc" }),
    });
    await shown.waitFor("id:work-card-one", { timeout: 10_000 });
    await shown.wait(800);
    expect(shown.shows("id:work-card-screen")).toBe(false);
  }, 25_000);

  test("a running Worker's own news (its activity, its output) does not ask the computer again", async () => {
    const writer = session("%1", { name: "Writer" });
    const shown = await app({ worker_sessions: [writer] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%1")], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(linux),
    });
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    const asked = () => shown.requests().filter((r) => r.url === `${BASE}/display`).length;
    const before = asked();
    for (let i = 0; i < 6; i++) {
      await shown.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...writer, activity: `Step ${i}`, last_output_lines: [`line ${i}`], updated_at: Date.now() + i } });
      await shown.wait(50);
    }
    await shown.wait(300);
    // Six updates asked six times before; the float's own 3 s poll may fall in this window once.
    expect(asked() - before).toBeLessThan(3);
    // It stopping is news: nothing runs, so nothing is asked or shown.
    await shown.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...writer, status: "done" } });
    await shown.waitUntil(() => !shown.shows("id:screen-float"), { timeout: 5_000 });
  }, 25_000);

  test("a Session's page shows its own Worker's screen only", async () => {
    const shown = await app({ worker_sessions: [session("%1", { name: "Writer" }), session("%2", { name: "Coder" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%2")], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(linux),
    });
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    await shown.host.send("links.open", { url: "mewla://sessions/%251" });
    await shown.waitUntil(() => !shown.shows("id:screen-float"), { timeout: 10_000 });
  }, 25_000);

  // That the Mac's is a watch, never a take-over, is core/screen/float.test.ts's "what opens a screen".
  test("an agent on the Mac's screen floats on an iPhone too; two screens take turns on the title", async () => {
    const mac = { supported: true, platform: "darwin", name: "Studio", disabled: false, approved: true, ready: true, keep_awake: false, permissions: [], video: "webrtc", agent: { worker: "%2", name: "Coder", since: "" }, agents: [{ worker: "%2", name: "Coder", since: "" }] };
    const shown = await app({ worker_sessions: [session("%2", { name: "Coder" }), session("%1", { name: "Writer" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%1")], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(mac),
    }, "ios");
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    await shown.waitUntil(() => shown.texts().some((text) => /1\/2$/.test(text)), { timeout: 10_000 });
    const before = shown.texts().find((text) => /\/2$/.test(text));
    await shown.press("id:screen-float-title");
    await shown.waitUntil(() => shown.texts().some((text) => /2\/2$/.test(text)));
    expect(shown.texts().find((text) => /\/2$/.test(text))).not.toBe(before);
  }, 25_000);

  test("Hide closes one Worker's window and it stays closed while that Worker keeps using its screen; the other's still floats; Screen brings it back", async () => {
    const shown = await app({ worker_sessions: [session("%2", { name: "Coder" }), session("%1", { name: "Writer" })] }, {
      [`GET ${BASE}/display`]: json({ displays: [display("%1"), display("%2")], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(linux),
    });
    await shown.waitUntil(() => shown.texts().some((text) => /^Writer {2}1\/2$/.test(text)), { timeout: 10_000 });
    await shown.press("id:screen-float-hide");
    await shown.waitUntil(() => shown.texts().includes("Coder"));
    // Through a few reads of the computer, with the Worker still on its display, it stays closed.
    const reads = shown.requests().filter((r) => r.url === `${BASE}/display`).length;
    await shown.waitUntil(() => shown.requests().filter((r) => r.url === `${BASE}/display`).length >= reads + 2, { timeout: 10_000 });
    expect(shown.texts().some((text) => text.startsWith("Writer"))).toBe(false);
    expect(Object.entries(shown.storage()).find(([key]) => key.startsWith("mewla:screen_float_hidden:"))?.[1]).toBe(JSON.stringify(["display:%1"]));
    // Screen, its own page, brings it back.
    await shown.press("id:screen-float-open");
    await shown.waitFor("id:screen-page", { timeout: 10_000 });
    await shown.back();
    await shown.waitUntil(() => shown.texts().some((text) => /^Writer {2}1\/2$/.test(text)), { timeout: 10_000 });
    expect(Object.entries(shown.storage()).find(([key]) => key.startsWith("mewla:screen_float_hidden:"))?.[1]).toBe("[]");
  }, 25_000);

  test("hidden stays hidden after the app starts again", async () => {
    const network = {
      [`GET ${BASE}/display`]: json({ displays: [display("%1")], available: true, video: "webrtc" }),
      [`GET ${BASE}/screen?video=webrtc`]: json(linux),
    };
    const first = await app({ worker_sessions: [session("%1", { name: "Writer" })] }, network);
    await first.waitFor("id:screen-float", { timeout: 10_000 });
    await first.press("id:screen-float-hide");
    await first.waitUntil(() => !first.shows("id:screen-float"));
    const kept = Object.entries(first.storage()).filter(([key]) => key.startsWith("mewla:screen_float_hidden:"));
    expect(kept).toHaveLength(1);
    const again = await open(APP, { host: phoneHost({ brain: {}, worker_sessions: [session("%1", { name: "Writer" })] }), storage: { ...paired, ...Object.fromEntries(kept) }, platform: "android", network: { ...expoTokenService, ...network } });
    await again.waitFor("id:composer");
    await again.waitUntil(() => again.requests().filter((r) => r.url === `${BASE}/display`).length >= 2, { timeout: 10_000 });
    expect(again.shows("id:screen-float")).toBe(false);
  }, 25_000);

  test("a hidden phone comes back when its agent asks the person to take over; hidden again in that request, it stays hidden until the next one", async () => {
    const working = {
      supported: true, sdk: 35, enabled: true, link: true, link_state: "online", developer_options: true, wireless_debugging: true, wifi: true, paired: true, helper: true,
      problem: null, accessibility: true, restricted: false, self_healing: true, guide: null, notifications: true, state: "ready", w: 720, h: 1600, display_info: null,
      agent: "Writer", handoff: null as string | null, drawn: true, opening: null, launch_failed: null,
    };
    let status = working;
    const handlers = { "phone.status": () => status, "phone.follow": () => null, "phone.setPipAuto": () => null };
    const shown = await open(APP, { host: phoneHost({}, handlers), storage: paired, platform: "android", network: expoTokenService });
    await shown.waitFor("id:composer");
    const say = async (next: typeof working) => {
      status = next;
      await shown.host.send("phone.status", next);
    };
    await shown.waitFor("id:screen-float", { timeout: 10_000 });
    await shown.press("id:screen-float-hide");
    await shown.waitUntil(() => !shown.shows("id:screen-float"));
    // The agent asks for the person: the window is where they act, so it comes back, ringed.
    await say({ ...working, state: "person_in_control", handoff: "Sign in to Instagram" });
    await shown.waitFor("id:screen-float-attention", { timeout: 5_000 });
    // Hidden again in that request, it stays hidden through it, and after it.
    await shown.press("id:screen-float-hide");
    await shown.waitUntil(() => !shown.shows("id:screen-float"));
    await say({ ...working, state: "person_in_control", handoff: "Sign in to Instagram" });
    await shown.wait(300);
    expect(shown.shows("id:screen-float")).toBe(false);
    await say(working);
    await shown.wait(300);
    expect(shown.shows("id:screen-float")).toBe(false);
    // The next request brings it back, even asking the same thing.
    await say({ ...working, state: "person_in_control", handoff: "Sign in to Instagram" });
    await shown.waitFor("id:screen-float-attention", { timeout: 5_000 });
  }, 25_000);

  describe("its live view", () => {
    const VIEWER = "ws://127.0.0.1:1/display/viewer";
    const sockets = { "app.socketOpened": () => null, "app.socketClosed": () => null, "app.socketSent": () => null };
    const viewed = () => open("./testing/FloatViewerApp.tsx", {
      host: phoneHost({ brain: {}, worker_sessions: [session("%1", { name: "Writer" })] }, sockets),
      storage: paired,
      platform: "android",
      network: { ...expoTokenService, [`GET ${BASE}/display`]: json({ displays: [display("%1")], available: true, video: "webrtc" }), [`GET ${BASE}/screen?video=webrtc`]: json(linux) },
    });
    type App = Awaited<ReturnType<typeof viewed>>;
    const opened = (shown: App) => shown.host.posts().filter((p) => p.name === "app.socketOpened" && (p.data as { url: string }).url.startsWith(VIEWER)).length;

    test("a dropped link comes back by itself: no button to tap", async () => {
      const shown = await viewed();
      await shown.waitFor("id:screen-float", { timeout: 10_000 });
      await shown.waitUntil(() => opened(shown) === 1, { timeout: 10_000 });
      await shown.host.send("app.viewer", { type: "connected", width: 1280, height: 800 });
      await shown.waitUntil(() => !shown.shows("id:screen-float-cover"));
      await shown.host.send("app.socketEnd", { reason: "" });
      await shown.waitFor("id:screen-float-cover");
      expect(shown.shows("id:screen-float-reconnect")).toBe(false);
      await shown.waitUntil(() => opened(shown) === 2, { timeout: 5_000 });
      await shown.host.send("app.viewer", { type: "connected", width: 1280, height: 800 });
      await shown.waitUntil(() => !shown.shows("id:screen-float-cover"));
      expect(shown.shows("id:screen-float")).toBe(true);
    }, 25_000);

    test("a view that cannot come up closes the window instead of leaving it dark", async () => {
      const shown = await viewed();
      await shown.waitFor("id:screen-float", { timeout: 10_000 });
      await shown.waitUntil(() => opened(shown) === 1, { timeout: 10_000 });
      await shown.host.send("app.socketEnd", { reason: "" });
      await shown.waitUntil(() => opened(shown) === 2, { timeout: 5_000 });
      await shown.host.send("app.socketEnd", { reason: "" });
      await shown.waitUntil(() => !shown.shows("id:screen-float"), { timeout: 5_000 });
      // The Worker is still on its display: the window stays closed through the next reads.
      const reads = shown.requests().filter((r) => r.url === `${BASE}/display`).length;
      await shown.waitUntil(() => shown.requests().filter((r) => r.url === `${BASE}/display`).length >= reads + 1, { timeout: 10_000 });
      expect(shown.shows("id:screen-float")).toBe(false);
      expect(opened(shown)).toBe(2);
    }, 25_000);
  });

  /** Two agents' displays on a manual clock: taps and drags land at the times the test says. */
  const two = () => app({ worker_sessions: [session("%2", { name: "Coder" }), session("%1", { name: "Writer" })] }, {
    [`GET ${BASE}/display`]: json({ displays: [display("%1"), display("%2")], available: true, video: "webrtc" }),
    [`GET ${BASE}/screen?video=webrtc`]: json(linux),
  }, "android", { clock: {} });
  const on = (shown: Awaited<ReturnType<typeof app>>, n: number) => shown.texts().some((text) => new RegExp(`${n}/2$`).test(text));

  test("a double tap on the title only changes the size: the first tap does not go to the next screen", async () => {
    const shown = await two();
    await shown.waitUntil(() => on(shown, 1), { timeout: 10_000 });
    await shown.press("id:screen-float-title");
    await shown.press("id:screen-float-title");
    await shown.clock!.runFor(1_000);
    expect(on(shown, 1)).toBe(true);
    expect(shown.storage()["mewla:phone_float_size"]).toBe("0.8");
    // One tap goes on once no second one follows.
    await shown.press("id:screen-float-title");
    expect(on(shown, 1)).toBe(true);
    await shown.clock!.runFor(1_000);
    expect(on(shown, 2)).toBe(true);
  }, 25_000);

  test("dragged by its bar and let go, the window goes to the nearest corner and stays there", async () => {
    const shown = await two();
    await shown.waitUntil(() => on(shown, 1), { timeout: 10_000 });
    await shown.drag("id:screen-float-title", { x: -300, y: -500 });
    await shown.clock!.runFor(1_000);
    expect(shown.storage()["mewla:phone_float_corner"]).toBe("top-left");
    // A drag is not a tap: still the same screen.
    expect(on(shown, 1)).toBe(true);
  }, 25_000);
});
